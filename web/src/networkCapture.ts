import type { NetworkCaptureEvent, NetworkCaptureSample, NetworkPeerSnapshot, NetworkPodIdentity } from './types'

/** Defines the default rolling chart horizon for one live capture session. */
export const NETWORK_CHART_WINDOW_SECONDS = 60

/** Describes the directional relationship between a peer and the selected Pod. */
export type NetworkPeerDirection = 'inbound' | 'outbound' | 'unknown'

/** Contains one browser chart point derived from exact interface counters. */
export interface NetworkChartPoint {
  at: number
  inboundBytesPerSecond: number
  outboundBytesPerSecond: number
  inboundPacketsPerSecond: number
  outboundPacketsPerSecond: number
}

/** Contains one display-ready peer row with resolved or unresolved identity. */
export interface NetworkPeerRow {
  key: string
  direction: NetworkPeerDirection
  peerAddress: string
  peerLabel: string
  source: string
  destination: string
  sourcePort?: number
  destinationPort?: number
  protocol: string
  packets: number
  payloadBytes: number
  lastSeen: string
  loopback: boolean
}

/** Holds the bounded browser state for one active or completed capture session. */
export interface NetworkCaptureModel {
  status: string
  message: string
  samples: NetworkChartPoint[]
  peers: NetworkPeerSnapshot[]
  maxDurationSeconds: number
  inboundBytesTotal: number
  outboundBytesTotal: number
  inboundPacketsTotal: number
  outboundPacketsTotal: number
  inboundErrors: number
  outboundErrors: number
  inboundDrops: number
  outboundDrops: number
}

/** Creates an empty live network capture model. */
export function createNetworkCaptureModel(): NetworkCaptureModel {
  return {
    status: 'starting',
    message: '',
    samples: [],
    peers: [],
    maxDurationSeconds: 0,
    inboundBytesTotal: 0,
    outboundBytesTotal: 0,
    inboundPacketsTotal: 0,
    outboundPacketsTotal: 0,
    inboundErrors: 0,
    outboundErrors: 0,
    inboundDrops: 0,
    outboundDrops: 0,
  }
}

/** Reduces one server event while retaining only the configured rolling chart window. */
export function reduceNetworkCaptureEvent(model: NetworkCaptureModel, event: NetworkCaptureEvent, now = Date.now()): NetworkCaptureModel {
  const next: NetworkCaptureModel = { ...model, samples: [...model.samples], peers: [...model.peers] }
  if (event.state) next.status = event.state
  if (event.message) next.message = event.message
  if (event.maxDurationSeconds) next.maxDurationSeconds = event.maxDurationSeconds
  if (event.type === 'status') next.status = event.state || 'capturing'
  if (event.type === 'error') next.status = event.state || 'error'
  if (event.type === 'closed') next.status = event.state || 'closed'
  if (event.sample) applyNetworkSample(next, event.sample, event.at ? Date.parse(event.at) : now)
  if (event.peers) next.peers = event.peers
  return trimNetworkCaptureModel(next, now)
}

/** Adds one counter sample and updates the current session totals. */
export function applyNetworkSample(model: NetworkCaptureModel, sample: NetworkCaptureSample, at: number): void {
  model.samples.push({
    at: Number.isFinite(at) ? at : Date.now(),
    inboundBytesPerSecond: finiteNumber(sample.inboundBytesPerSecond),
    outboundBytesPerSecond: finiteNumber(sample.outboundBytesPerSecond),
    inboundPacketsPerSecond: finiteNumber(sample.inboundPacketsPerSecond),
    outboundPacketsPerSecond: finiteNumber(sample.outboundPacketsPerSecond),
  })
  model.inboundBytesTotal = Math.max(model.inboundBytesTotal, finiteNumber(sample.inboundBytesTotal))
  model.outboundBytesTotal = Math.max(model.outboundBytesTotal, finiteNumber(sample.outboundBytesTotal))
  model.inboundPacketsTotal = Math.max(model.inboundPacketsTotal, finiteNumber(sample.inboundPacketsTotal))
  model.outboundPacketsTotal = Math.max(model.outboundPacketsTotal, finiteNumber(sample.outboundPacketsTotal))
  model.inboundErrors = Math.max(model.inboundErrors, finiteNumber(sample.inboundErrors))
  model.outboundErrors = Math.max(model.outboundErrors, finiteNumber(sample.outboundErrors))
  model.inboundDrops = Math.max(model.inboundDrops, finiteNumber(sample.inboundDrops))
  model.outboundDrops = Math.max(model.outboundDrops, finiteNumber(sample.outboundDrops))
}

/** Trims samples to the 60-second rolling window and removes malformed timestamps. */
export function trimNetworkCaptureModel(model: NetworkCaptureModel, now = Date.now()): NetworkCaptureModel {
  const earliest = now - NETWORK_CHART_WINDOW_SECONDS * 1000
  model.samples = model.samples.filter((sample) => Number.isFinite(sample.at) && sample.at >= earliest)
  return model
}

/** Converts one peer snapshot into a display row relative to the selected Pod. */
export function networkPeerRow(snapshot: NetworkPeerSnapshot, identities: Record<string, NetworkPodIdentity>, podIPs: string[], podName: string): NetworkPeerRow {
  const sourceIsPod = podIPs.includes(snapshot.source)
  const destinationIsPod = podIPs.includes(snapshot.destination)
  const direction: NetworkPeerDirection = sourceIsPod && !destinationIsPod ? 'outbound' : destinationIsPod && !sourceIsPod ? 'inbound' : 'unknown'
  const peerAddress = direction === 'outbound' ? snapshot.destination : direction === 'inbound' ? snapshot.source : `${snapshot.source} -> ${snapshot.destination}`
  const loopback = isLoopbackAddress(snapshot.source) && isLoopbackAddress(snapshot.destination)
  const identity = identities[peerAddress]
  const peerLabel = loopback ? 'localhost' : identity ? `${identity.namespace}/${identity.name}` : peerAddress === podName ? podName : peerAddress
  return {
    key: `${snapshot.source}|${snapshot.destination}|${snapshot.sourcePort || 0}|${snapshot.destinationPort || 0}|${snapshot.protocol}`,
    direction,
    peerAddress,
    peerLabel,
    source: snapshot.source,
    destination: snapshot.destination,
    sourcePort: snapshot.sourcePort,
    destinationPort: snapshot.destinationPort,
    protocol: snapshot.protocol,
    packets: snapshot.packets,
    payloadBytes: snapshot.payloadBytes,
    lastSeen: snapshot.lastSeen,
    loopback,
  }
}

/** Collapses source-port variants into logical peer, direction, protocol, and destination-port rows. */
export function aggregateNetworkPeerRows(rows: NetworkPeerRow[], includeLoopback = false): NetworkPeerRow[] {
  const grouped = new Map<string, NetworkPeerRow>()
  rows.forEach((row) => {
    if (row.loopback && !includeLoopback) return
    const key = `${row.direction}|${row.peerAddress}|${row.protocol}|${row.destinationPort || 0}`
    const existing = grouped.get(key)
    if (!existing) {
      grouped.set(key, { ...row, key, sourcePort: undefined })
      return
    }
    existing.packets += row.packets
    existing.payloadBytes += row.payloadBytes
    if (Date.parse(row.lastSeen) > Date.parse(existing.lastSeen)) existing.lastSeen = row.lastSeen
  })
  return Array.from(grouped.values()).sort((left, right) => right.packets - left.packets || right.payloadBytes - left.payloadBytes || left.peerLabel.localeCompare(right.peerLabel))
}

/** Returns whether an address belongs to the Pod-local loopback interface. */
export function isLoopbackAddress(address: string): boolean {
  const normalized = address.trim().toLowerCase()
  return normalized === '::1' || normalized === '0:0:0:0:0:0:0:1' || normalized === 'localhost' || normalized.startsWith('127.')
}

/** Converts an event-stream frame into one typed network event. */
export function parseNetworkEventFrame(frame: string): NetworkCaptureEvent | null {
  const data = frame.split('\n').filter((line) => line.startsWith('data:')).map((line) => line.slice(5).trim()).join('\n')
  if (!data) return null
  try {
    const parsed = JSON.parse(data) as NetworkCaptureEvent
    return parsed && typeof parsed.type === 'string' ? parsed : null
  } catch {
    return null
  }
}

/** Returns a finite non-negative number for untrusted stream fields. */
function finiteNumber(value: number): number {
  return Number.isFinite(value) && value >= 0 ? value : 0
}
