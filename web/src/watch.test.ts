import {
  ReliableWatchController,
  WATCH_STABLE_DELAY_MS,
  type WatchConnection,
  type WatchConnectionHandlers,
  freshnessStateAfter,
  parseWatchMessage,
  reconnectDelay,
} from './watch'
import type { FreshnessState } from './watch'

/** Fails a focused watch-model assertion with a readable message. */
function assertWatch(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises state classification and the bounded exponential retry schedule. */
function runWatchStateModelTests(): void {
  const states: FreshnessState[] = ['live', 'reconnecting', 'stale', 'refreshing', 'unavailable']
  assertWatch(states.includes(freshnessStateAfter('watch-opened', true)), 'watch-opened did not produce a known freshness state')
  assertWatch(freshnessStateAfter('watch-closed', true) === 'reconnecting', 'closed watches did not enter reconnecting state')
  assertWatch(freshnessStateAfter('watch-error', true) === 'stale', 'watch errors did not mark existing data stale')
  assertWatch(freshnessStateAfter('refresh-failed', false) === 'unavailable', 'failed initial refresh did not become unavailable')
  assertWatch(parseWatchMessage('{"type":"CLOSED"}').reason === 'watch-closed', 'closed SSE signal was not classified')
  assertWatch(parseWatchMessage('{"type":"ERROR","error":{"reason":"resource-version-expired"}}').reason === 'resource-version-expired', 'expired SSE signal was not classified')
  assertWatch(parseWatchMessage('{"type":"MODIFIED","object":{}}').kind === 'event', 'normal Kubernetes event was not classified')
  assertWatch(reconnectDelay(1) === 250 && reconnectDelay(2) === 500 && reconnectDelay(6) === 8000 && reconnectDelay(20) === 8000, 'retry delay did not use the bounded exponential schedule')
}

/** Creates a controllable EventSource substitute for deterministic controller tests. */
function fakeWatchConnection(): WatchConnection & { emitOpen: () => void; emitMessage: (data: string) => void; emitError: () => void; closed: boolean } {
  let handlers: WatchConnectionHandlers | null = null
  const connection = {
    closed: false,
    setHandlers: (nextHandlers: WatchConnectionHandlers) => { handlers = nextHandlers },
    close: () => { connection.closed = true },
    emitOpen: () => handlers?.open(),
    emitMessage: (data: string) => handlers?.message(data),
    emitError: () => handlers?.error(),
  }
  return connection
}

/** Creates a manually advanced timer scheduler for cancellation and backoff tests. */
function fakeScheduler() {
  const timers = new Map<number, { callback: () => void; delay: number }>()
  let nextTimer = 1
  return {
    timers,
    schedule: (callback: () => void, delay: number) => {
      const id = nextTimer++
      timers.set(id, { callback, delay })
      return id
    },
    cancel: (id: number) => { timers.delete(id) },
    runNext: () => {
      const next = timers.entries().next().value as [number, { callback: () => void; delay: number }] | undefined
      if (!next) return undefined
      timers.delete(next[0])
      next[1].callback()
      return next[1].delay
    },
  }
}

/** Lets promise callbacks settle without relying on browser timers. */
async function settleWatchPromises(): Promise<void> {
  await Promise.resolve()
  await Promise.resolve()
}

/** Exercises relist-before-reconnect, newest-resource-version selection, backoff, and cancellation. */
async function runWatchControllerModelTests(): Promise<void> {
  runWatchStateModelTests()
  const scheduler = fakeScheduler()
  const connections: Array<WatchConnection & { emitOpen: () => void; emitMessage: (data: string) => void; emitError: () => void; closed: boolean }> = []
  const resolvers: Array<(value: { resourceVersion: string; items: string[] }) => void> = []
  const rejecters: Array<(reason: unknown) => void> = []
  const signals: AbortSignal[] = []
  const states: string[] = []
  const loadedVersions: string[] = []
  const controller = new ReliableWatchController({
    load: (signal) => new Promise<{ resourceVersion: string; items: string[] }>((resolve, reject) => {
      signals.push(signal)
      resolvers.push(resolve)
      rejecters.push(reject)
    }),
    getResourceVersion: (data) => data.resourceVersion,
    watchUrl: (resourceVersion) => `/watch?resourceVersion=${resourceVersion}`,
    open: () => {
      const connection = fakeWatchConnection()
      connections.push(connection)
      return connection
    },
    onData: (data) => loadedVersions.push(data.resourceVersion),
    onState: (snapshot) => states.push(snapshot.state),
    now: () => 1700000000000 + loadedVersions.length,
    schedule: scheduler.schedule,
    cancel: scheduler.cancel,
  })

  controller.start()
  assertWatch(resolvers.length === 1 && states[0] === 'refreshing', 'controller did not start with one refreshing relist')
  resolvers[0]({ resourceVersion: '12', items: ['first'] })
  await settleWatchPromises()
  assertWatch(connections.length === 1 && loadedVersions[0] === '12', 'initial relist did not open one watch')
  connections[0].emitOpen()
  assertWatch(controller.getSnapshot().state === 'live', 'watch open did not become live')

  connections[0].emitMessage('{"type":"MODIFIED","object":{}}')
  connections[0].emitMessage('{"type":"MODIFIED","object":{}}')
  assertWatch(resolvers.length === 2 && connections[0].closed, 'normal events created duplicate relists or left the old watch open')
  resolvers[1]({ resourceVersion: '13', items: ['second'] })
  await settleWatchPromises()
  assertWatch(connections.length === 2 && loadedVersions[1] === '13', 'relist reused an old resource version')
  assertWatch(controller.getSnapshot().lastSuccessfulRefresh !== null, 'successful relist did not record a UI timestamp')
  connections[1].emitOpen()

  connections[1].emitMessage('{"type":"CLOSED"}')
  assertWatch(controller.getSnapshot().state === 'reconnecting' && scheduler.timers.size === 1, 'watch closure did not schedule reconnecting recovery')
  assertWatch(scheduler.runNext() === 250 && resolvers.length === 3 && controller.getSnapshot().state === 'refreshing', 'closed-watch recovery did not relist with the initial backoff')
  resolvers[2]({ resourceVersion: '14', items: ['recovered'] })
  await settleWatchPromises()
  assertWatch(connections.length === 3 && loadedVersions[2] === '14', 'closed-watch recovery did not open a watch from the new list')
  connections[2].emitOpen()

  connections[2].emitError()
  assertWatch(controller.getSnapshot().state === 'reconnecting' && scheduler.timers.size === 1, 'network loss did not schedule reconnecting recovery')
  assertWatch(scheduler.runNext() === 500 && resolvers.length === 4 && controller.getSnapshot().state === 'refreshing', 'network recovery did not increase backoff before relisting')
  resolvers[3]({ resourceVersion: '15', items: ['network-recovered'] })
  await settleWatchPromises()
  assertWatch(connections.length === 4 && loadedVersions[3] === '15', 'network recovery did not open a watch from the relisted version')
  connections[3].emitOpen()

  connections[3].emitError()
  assertWatch(scheduler.runNext() === 1000 && resolvers.length === 5, 'second network loss did not continue increasing backoff')
  rejecters[4](new Error('network unavailable'))
  await settleWatchPromises()
  assertWatch(controller.getSnapshot().state === 'stale' && scheduler.timers.size === 1 && scheduler.runNext() === 2000, 'failed relist did not mark data stale or increase backoff')

  assertWatch(resolvers.length === 6 && controller.getSnapshot().state === 'refreshing', 'backoff timer did not start the next relist')
  controller.stop()
  assertWatch(scheduler.timers.size === 0 && signals[5].aborted, 'stop did not cancel the retry timer and in-flight relist')
  resolvers[5]({ resourceVersion: '16', items: ['late'] })
  await settleWatchPromises()
  assertWatch(connections.length === 4, 'cancelled controller opened a late watch')
}

/** Exercises repeated quick watch flapping, the eight-second cap, and stable-live reset. */
async function runWatchBackoffModelTests(): Promise<void> {
  const scheduler = fakeScheduler()
  const connections: Array<ReturnType<typeof fakeWatchConnection>> = []
  let version = 0
  const controller = new ReliableWatchController({
    load: async () => ({ resourceVersion: String(++version) }),
    getResourceVersion: (data) => data.resourceVersion,
    watchUrl: (resourceVersion) => `/watch?resourceVersion=${resourceVersion}`,
    open: () => {
      const connection = fakeWatchConnection()
      connections.push(connection)
      return connection
    },
    onData: () => undefined,
    onState: () => undefined,
    schedule: scheduler.schedule,
    cancel: scheduler.cancel,
  })

  controller.start()
  await settleWatchPromises()
  connections[0].emitOpen()
  const expectedDelays = [250, 500, 1000, 2000, 4000, 8000, 8000, 8000]
  for (const expectedDelay of expectedDelays) {
    const connection = connections[connections.length - 1]
    connection.emitError()
    assertWatch(scheduler.runNext() === expectedDelay, `watch flap used the wrong retry delay; expected ${expectedDelay}ms`)
    await settleWatchPromises()
    connections[connections.length - 1].emitOpen()
  }
  assertWatch(scheduler.runNext() === WATCH_STABLE_DELAY_MS && controller.getSnapshot().attempt === 0, 'stable live watch did not reset accumulated backoff')
  connections[connections.length - 1].emitError()
  assertWatch(scheduler.runNext() === 250, 'stable live watch did not restore fast recovery')
  controller.stop()
}

/** Exercises the safe initial permission signal and verifies it becomes unavailable without retrying. */
async function runPermissionDeniedModelTests(): Promise<void> {
  const scheduler = fakeScheduler()
  const connection = fakeWatchConnection()
  const controller = new ReliableWatchController({
    load: async () => ({ resourceVersion: '12' }),
    getResourceVersion: (data) => data.resourceVersion,
    watchUrl: (resourceVersion) => `/watch?resourceVersion=${resourceVersion}`,
    open: () => connection,
    onData: () => undefined,
    onState: () => undefined,
    schedule: scheduler.schedule,
    cancel: scheduler.cancel,
  })

  controller.start()
  await settleWatchPromises()
  connection.emitMessage('{"type":"ERROR","error":{"reason":"permission-denied","relist":false}}')
  assertWatch(controller.getSnapshot().state === 'unavailable' && scheduler.timers.size === 0, 'permission-denied watch did not become unavailable without a retry timer')
  controller.stop()
}

void runWatchControllerModelTests()
void runWatchBackoffModelTests()
void runPermissionDeniedModelTests()
