import { aggregateNetworkPeerRows, createNetworkCaptureModel, isLoopbackAddress, networkPeerRow, parseNetworkEventFrame, reduceNetworkCaptureEvent } from './networkCapture'

/** Fails a focused network-capture model assertion with a readable message. */
function assertNetwork(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises rolling charts, stream parsing, and Pod-relative peer labels. */
export function runNetworkCaptureModelTests(): void {
  const initial = createNetworkCaptureModel()
  const status = parseNetworkEventFrame('data: {"type":"status","state":"capturing","maxDurationSeconds":600}\n\n')
  assertNetwork(status?.type === 'status' && status.maxDurationSeconds === 600, 'network SSE status frame was not parsed')
  const first = reduceNetworkCaptureEvent(initial, status || { type: 'status' }, 100000)
  const sample = reduceNetworkCaptureEvent(first, {
    type: 'sample',
    at: new Date(100000).toISOString(),
    sample: {
      inboundBytesPerSecond: 120,
      outboundBytesPerSecond: 240,
      inboundPacketsPerSecond: 3,
      outboundPacketsPerSecond: 4,
      inboundBytesTotal: 1200,
      outboundBytesTotal: 2400,
      inboundPacketsTotal: 30,
      outboundPacketsTotal: 40,
      inboundErrors: 1,
      outboundErrors: 2,
      inboundDrops: 3,
      outboundDrops: 4,
    },
  }, 100000)
  assertNetwork(sample.status === 'capturing' && sample.samples.length === 1 && sample.outboundBytesTotal === 2400, 'network sample did not update the live model')
  const old = reduceNetworkCaptureEvent(sample, {
    type: 'sample',
    at: new Date(100000 - 61000).toISOString(),
    sample: {
      inboundBytesPerSecond: 1,
      outboundBytesPerSecond: 1,
      inboundPacketsPerSecond: 1,
      outboundPacketsPerSecond: 1,
      inboundBytesTotal: 1300,
      outboundBytesTotal: 2500,
      inboundPacketsTotal: 31,
      outboundPacketsTotal: 41,
      inboundErrors: 1,
      outboundErrors: 2,
      inboundDrops: 3,
      outboundDrops: 4,
    },
  }, 100000)
  assertNetwork(old.samples.length === 1, 'network chart did not trim samples outside the rolling window')
  const row = networkPeerRow({ source: '10.0.0.8', destination: '10.0.0.9', sourcePort: 8080, destinationPort: 42122, protocol: 'TCP', packets: 4, payloadBytes: 32, lastSeen: new Date(100000).toISOString() }, { '10.0.0.9': { ip: '10.0.0.9', name: 'worker', namespace: 'default' } }, ['10.0.0.8'], 'api')
  assertNetwork(row.direction === 'outbound' && row.peerLabel === 'default/worker' && row.destinationPort === 42122, 'network peer direction or identity was not resolved')
  const loopback = networkPeerRow({ source: '127.0.0.1', destination: '127.0.0.1', sourcePort: 50001, destinationPort: 8080, protocol: 'TCP', packets: 2, payloadBytes: 0, lastSeen: new Date(100000).toISOString() }, {}, ['10.0.0.8'], 'api')
  const aggregated = aggregateNetworkPeerRows([row, row, loopback])
  assertNetwork(loopback.loopback && isLoopbackAddress('127.0.0.1') && aggregated.length === 1 && aggregated[0].packets === 8, 'logical peer aggregation did not hide loopback or combine source-port variants')
}

runNetworkCaptureModelTests()
