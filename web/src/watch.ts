/** Identifies the operator-visible freshness state of a watched panel. */
export type FreshnessState = 'live' | 'reconnecting' | 'stale' | 'refreshing' | 'unavailable'

/** Defines how long a watch must remain open before recovery backoff resets. */
export const WATCH_STABLE_DELAY_MS = 10000

/** Identifies the safe failure reasons understood from the watch SSE protocol. */
export type WatchFailureReason = 'network' | 'watch-closed' | 'resource-version-expired' | 'permission-denied' | 'watch-error' | 'refresh-error'

/** Describes the lifecycle result parsed from one watch SSE data frame. */
export interface WatchMessage {
  kind: 'event' | 'closed' | 'error'
  reason?: WatchFailureReason
}

/** Contains UI-only freshness metadata for one resource or Event panel. */
export interface WatchSnapshot {
  state: FreshnessState
  attempt: number
  hasData: boolean
  resourceVersion: string
  lastSuccessfulRefresh: number | null
  message?: string
}

/** Defines the callbacks used to connect an EventSource-like browser stream. */
export interface WatchConnectionHandlers {
  open: () => void
  message: (data: string) => void
  error: () => void
}

/** Represents the small EventSource surface required by the reconnect controller. */
export interface WatchConnection {
  setHandlers: (handlers: WatchConnectionHandlers) => void
  close: () => void
}

/** Defines the injectable I/O and clock seams used by the reconnect controller. */
export interface ReliableWatchOptions<T> {
  load: (signal: AbortSignal) => Promise<T>
  getResourceVersion: (data: T) => string
  watchUrl: (resourceVersion: string) => string
  onData: (data: T, snapshot: WatchSnapshot) => void
  onState: (snapshot: WatchSnapshot) => void
  open?: (url: string) => WatchConnection
  now?: () => number
  schedule?: (callback: () => void, delayMilliseconds: number) => ReturnType<typeof setTimeout>
  cancel?: (timer: ReturnType<typeof setTimeout>) => void
}

/** Identifies the state transition used by the pure freshness model. */
export type WatchLifecycleEvent = 'refresh-started' | 'refresh-succeeded' | 'watch-opened' | 'watch-closed' | 'watch-error' | 'refresh-failed'

/** Returns the initial model state before a panel has read Kubernetes data. */
export function initialWatchSnapshot(): WatchSnapshot {
  return { state: 'refreshing', attempt: 0, hasData: false, resourceVersion: '', lastSuccessfulRefresh: null }
}

/** Calculates a bounded exponential retry delay for a one-based retry attempt. */
export function reconnectDelay(attempt: number): number {
  const boundedAttempt = Math.max(1, Math.floor(attempt))
  return Math.min(8000, 250 * 2 ** (boundedAttempt - 1))
}

/** Applies one lifecycle event to the UI freshness state without scheduling work. */
export function freshnessStateAfter(event: WatchLifecycleEvent, hasData: boolean): FreshnessState {
  switch (event) {
    case 'refresh-started':
      return 'refreshing'
    case 'refresh-succeeded':
      return 'reconnecting'
    case 'watch-opened':
      return 'live'
    case 'watch-closed':
      return hasData ? 'reconnecting' : 'unavailable'
    case 'watch-error':
    case 'refresh-failed':
      return hasData ? 'stale' : 'unavailable'
  }
}

/** Parses a safe watch frame while treating normal Kubernetes events uniformly. */
export function parseWatchMessage(data: string): WatchMessage {
  try {
    const payload = JSON.parse(data) as { type?: unknown; error?: { reason?: unknown } }
    if (payload.type === 'CLOSED') return { kind: 'closed', reason: 'watch-closed' }
    if (payload.type === 'ERROR') {
      const reason = payload.error?.reason
      if (reason === 'resource-version-expired' || reason === 'permission-denied' || reason === 'watch-error' || reason === 'watch-closed') {
        return { kind: 'error', reason }
      }
      return { kind: 'error', reason: 'watch-error' }
    }
  } catch {
    return { kind: 'error', reason: 'watch-error' }
  }
  return { kind: 'event' }
}

/** Returns a stable operator-facing description for a watch recovery condition. */
export function watchFailureMessage(reason: WatchFailureReason): string {
  switch (reason) {
    case 'resource-version-expired':
      return 'The resource version expired; Kubernetes data will be relisted before the watch restarts.'
    case 'watch-closed':
      return 'The Kubernetes watch closed; the panel is reconnecting after a fresh relist.'
    case 'network':
      return 'The Kubernetes watch connection was lost; the panel is reconnecting after a fresh relist.'
    case 'refresh-error':
      return 'The latest Kubernetes refresh failed; the last successful data remains visible while retrying.'
    case 'permission-denied':
      return 'Kubernetes denied this watch; live updates are unavailable for this panel.'
    default:
      return 'The Kubernetes watch reported an error; the panel is relisting before it retries.'
  }
}

/** Opens a native browser EventSource through the narrow connection interface. */
export function browserWatchConnection(url: string): WatchConnection {
  const source = new EventSource(url)
  return {
    setHandlers: (handlers) => {
      source.onopen = () => handlers.open()
      source.onmessage = (event) => handlers.message(event.data)
      source.onerror = () => handlers.error()
    },
    close: () => source.close(),
  }
}

/** Owns one relist, one SSE watch, one retry timer, and one cancellation boundary. */
export class ReliableWatchController<T> {
  private readonly options: ReliableWatchOptions<T>
  private active = false
  private generation = 0
  private stream: WatchConnection | null = null
  private timer: ReturnType<typeof setTimeout> | null = null
  private stableTimer: ReturnType<typeof setTimeout> | null = null
  private refreshAbort: AbortController | null = null
  private snapshot: WatchSnapshot = initialWatchSnapshot()

  /** Creates a controller with deterministic I/O seams suitable for browser or model tests. */
  constructor(options: ReliableWatchOptions<T>) {
    this.options = options
  }

  /** Starts with a relist and never reuses a resource version from an earlier run. */
  start(): void {
    this.stop()
    this.active = true
    const generation = this.generation
    this.snapshot = initialWatchSnapshot()
    this.emitState()
    this.refresh(generation)
  }

  /** Cancels the watch, retry timer, and in-flight relist without allowing late callbacks. */
  stop(): void {
    this.active = false
    this.generation += 1
    this.refreshAbort?.abort()
    this.refreshAbort = null
    if (this.timer !== null) {
      const timer = this.timer
      this.timer = null
      ;(this.options.cancel ?? clearTimeout)(timer)
    }
    this.clearStableTimer()
    this.closeStream()
  }

  /** Returns an immutable copy of the latest UI freshness model. */
  getSnapshot(): WatchSnapshot {
    return { ...this.snapshot }
  }

  /** Starts one relist when a normal watch event invalidates the current list snapshot. */
  private requestRefresh(generation: number, stream: WatchConnection): void {
    if (!this.isCurrent(generation) || this.stream !== stream || this.refreshAbort !== null) return
    this.closeStream()
    this.refresh(generation)
  }

  /** Performs the only refresh operation allowed for the current controller generation. */
  private refresh(generation: number): void {
    if (!this.isCurrent(generation) || this.refreshAbort !== null) return
    const abort = new AbortController()
    this.refreshAbort = abort
    this.setState({ state: 'refreshing', message: undefined })
    void this.options.load(abort.signal).then(
      (data) => this.completeRefresh(generation, abort, data),
      () => this.failRefresh(generation, abort),
    )
  }

  /** Commits a successful relist and opens the next watch from its newest resource version. */
  private completeRefresh(generation: number, abort: AbortController, data: T): void {
    if (!this.isCurrent(generation, abort)) return
    this.refreshAbort = null
    this.closeStream()
    const resourceVersion = this.options.getResourceVersion(data).trim()
    const timestamp = this.options.now?.() ?? Date.now()
    const refreshedSnapshot: WatchSnapshot = {
      ...this.snapshot,
      state: 'reconnecting',
      attempt: resourceVersion ? this.snapshot.attempt : this.snapshot.attempt + 1,
      hasData: true,
      resourceVersion,
      lastSuccessfulRefresh: timestamp,
      message: undefined,
    }
    this.snapshot = refreshedSnapshot
    this.options.onData(data, this.getSnapshot())
    this.emitState()
    if (!resourceVersion) {
      this.setState({ state: 'unavailable', message: 'Kubernetes returned no resource version for the live watch.' })
      this.scheduleRetry(generation, 'watch-error')
      return
    }
    this.openStream(generation, resourceVersion)
  }

  /** Records a failed relist and schedules the next relist with bounded backoff. */
  private failRefresh(generation: number, abort: AbortController): void {
    if (!this.isCurrent(generation, abort)) return
    this.refreshAbort = null
    this.setState({ state: freshnessStateAfter('refresh-failed', this.snapshot.hasData), attempt: this.snapshot.attempt + 1, message: watchFailureMessage('refresh-error') })
    this.scheduleRetry(generation, 'refresh-error')
  }

  /** Opens one SSE stream using the resource version produced by the latest successful relist. */
  private openStream(generation: number, resourceVersion: string): void {
    if (!this.isCurrent(generation)) return
    let stream: WatchConnection
    try {
      stream = (this.options.open ?? browserWatchConnection)(this.options.watchUrl(resourceVersion))
    } catch {
      this.handleWatchFailure(generation, 'network', null)
      return
    }
    this.stream = stream
    stream.setHandlers({
      open: () => {
        if (!this.isCurrent(generation) || this.stream !== stream) return
        this.setState({ state: 'live', message: undefined })
        this.scheduleStableReset(generation, stream)
      },
      message: (data) => {
        if (!this.isCurrent(generation) || this.stream !== stream) return
        const message = parseWatchMessage(data)
        if (message.kind === 'event') {
          this.requestRefresh(generation, stream)
          return
        }
        this.handleWatchFailure(generation, message.reason ?? 'watch-error', stream)
      },
      error: () => this.handleWatchFailure(generation, 'network', stream),
    })
  }

  /** Handles a closed or failed stream and guarantees a relist before the next watch. */
  private handleWatchFailure(generation: number, reason: WatchFailureReason, stream: WatchConnection | null): void {
    if (!this.isCurrent(generation) || (stream !== null && this.stream !== stream)) return
    this.closeStream()
    if (reason === 'permission-denied') {
      this.setState({ state: 'unavailable', message: watchFailureMessage(reason) })
      return
    }
    const lifecycle = reason === 'watch-closed' || reason === 'network' ? 'watch-closed' : 'watch-error'
    this.setState({ state: freshnessStateAfter(lifecycle, this.snapshot.hasData), attempt: this.snapshot.attempt + 1, message: watchFailureMessage(reason) })
    this.scheduleRetry(generation, reason)
  }

  /** Schedules one future relist and ignores duplicate scheduling requests. */
  private scheduleRetry(generation: number, reason: WatchFailureReason): void {
    if (!this.isCurrent(generation) || this.timer !== null) return
    const attempt = Math.max(1, this.snapshot.attempt)
    const delay = reconnectDelay(attempt)
    const schedule = this.options.schedule ?? ((callback: () => void, milliseconds: number) => setTimeout(callback, milliseconds))
    this.timer = schedule(() => {
      this.timer = null
      if (!this.isCurrent(generation)) return
      this.refresh(generation)
    }, delay)
    if (reason === 'refresh-error' && this.snapshot.state !== 'stale' && this.snapshot.state !== 'unavailable') {
      this.setState({ state: freshnessStateAfter('refresh-failed', this.snapshot.hasData) })
    }
  }

  /** Schedules the stable-live reset that clears accumulated flapping backoff. */
  private scheduleStableReset(generation: number, stream: WatchConnection): void {
    this.clearStableTimer()
    const schedule = this.options.schedule ?? ((callback: () => void, milliseconds: number) => setTimeout(callback, milliseconds))
    this.stableTimer = schedule(() => {
      this.stableTimer = null
      if (!this.isCurrent(generation) || this.stream !== stream) return
      this.setState({ attempt: 0 })
    }, WATCH_STABLE_DELAY_MS)
  }

  /** Closes the active stream and detaches it before invoking browser close behavior. */
  private closeStream(): void {
    const stream = this.stream
    this.stream = null
    this.clearStableTimer()
    stream?.close()
  }

  /** Cancels a pending stable-live timer without affecting the retry timer. */
  private clearStableTimer(): void {
    if (this.stableTimer === null) return
    const timer = this.stableTimer
    this.stableTimer = null
    ;(this.options.cancel ?? clearTimeout)(timer)
  }

  /** Returns whether an asynchronous callback still belongs to the active controller run. */
  private isCurrent(generation: number, abort?: AbortController): boolean {
    return this.active && generation === this.generation && (!abort || this.refreshAbort === abort)
  }

  /** Updates the model and publishes a defensive snapshot to the panel. */
  private setState(update: Partial<WatchSnapshot>): void {
    this.snapshot = { ...this.snapshot, ...update }
    this.emitState()
  }

  /** Publishes the current model through the configured UI callback. */
  private emitState(): void {
    this.options.onState(this.getSnapshot())
  }
}
