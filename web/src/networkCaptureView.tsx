import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  CircleDot,
  ChevronDown,
  LockKeyhole,
  LoaderCircle,
  Maximize2,
  Network,
  Pause,
  Play,
  Radio,
  Search,
  X,
} from 'lucide-react'
import { useEffect, useRef, useState, type MouseEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { networkCaptureUrl, prepareNetworkCapture, responseApiError } from './api'
import { aggregateNetworkPeerRows, createNetworkCaptureModel, networkPeerRow, parseNetworkEventFrame, reduceNetworkCaptureEvent, type NetworkCaptureModel, type NetworkChartPoint, type NetworkPeerRow } from './networkCapture'
import type { NetworkCaptureEvent, NetworkPodIdentity, ResourceObject } from './types'

/** Describes the Pod detail inputs needed to run one live network capture. */
export interface NetworkCaptureViewProps {
  context: string
  namespace: string
  pod: string
  object: ResourceObject
  writeEnabled: boolean
}

/** Identifies the network panel that is currently in focus mode. */
type NetworkFocusMode = 'bandwidth' | 'packets' | 'map' | 'table'

/** Defines one colored chart series and its sample field. */
type NetworkChartSeries = { label: string; color: string; key: keyof NetworkChartPoint }

/** Renders the Pod-local live network capture, charts, peer map, and safe capability states. */
export function NetworkCaptureView({ context, namespace, pod, object, writeEnabled }: NetworkCaptureViewProps) {
  const [model, setModel] = useState<NetworkCaptureModel>(() => createNetworkCaptureModel())
  const [peerIdentities, setPeerIdentities] = useState<Record<string, NetworkPodIdentity>>({})
  const [debugImage, setDebugImage] = useState('')
  const [includeLoopback, setIncludeLoopback] = useState(false)
  const [focusMode, setFocusMode] = useState<NetworkFocusMode | null>(null)
  const [chartPaused, setChartPaused] = useState(false)
  const [pausedSamples, setPausedSamples] = useState<NetworkChartPoint[] | null>(null)
  const focusReturnRef = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    const initial = createNetworkCaptureModel()
    if (!writeEnabled) {
      initial.status = 'write-required'
      initial.message = 'Enable write mode to attach the reusable network debug container.'
      setModel(initial)
      setPeerIdentities({})
      setDebugImage('')
      return
    }
    const controller = new AbortController()
    let active = true
    setModel(initial)
    setPeerIdentities({})
    setDebugImage('')

    /** Starts the reusable debug container and consumes its bounded live event stream. */
    async function startCapture() {
      try {
        const prepared = await prepareNetworkCapture(context, namespace, pod)
        if (!active) return
        setPeerIdentities(prepared.peers || {})
        setDebugImage(prepared.image)
        setModel((current) => ({ ...current, status: 'starting', message: prepared.created ? 'Attached the reusable network debug container. Waiting for live samples.' : 'Reusing the attached network debug container. Waiting for live samples.' }))
        const response = await fetch(networkCaptureUrl(context, namespace, pod), { signal: controller.signal })
        if (!response.ok) throw await responseApiError(response, `Network capture failed with ${response.status}`)
        await consumeNetworkEventStream(response, controller.signal, (event) => {
          if (!active) return
          setModel((current) => reduceNetworkCaptureEvent(current, event))
        })
      } catch (captureError) {
        if (!active || controller.signal.aborted) return
        const next = createNetworkCaptureModel()
        next.status = networkErrorState(captureError)
        next.message = captureError instanceof Error ? captureError.message : 'The live network capture could not start.'
        setModel(next)
      }
    }

    void startCapture()
    return () => {
      active = false
      controller.abort()
    }
  }, [context, namespace, pod, writeEnabled])

  const podIPs = podNetworkIPs(object)
  const rawPeers = model.peers.map((peer) => networkPeerRow(peer, peerIdentities, podIPs, pod))
  const peers = aggregateNetworkPeerRows(rawPeers, includeLoopback)
  const displaySamples = chartPaused && pausedSamples ? pausedSamples : model.samples
  const lastSample = model.samples[model.samples.length - 1]
  const statusLabel = networkStatusLabel(model.status)
  const live = model.status === 'capturing'
  if (model.status === 'write-required') {
    return <NetworkCaptureShell status={model.status} statusLabel={statusLabel} debugImage={debugImage}><NetworkCapabilityState icon={<LockKeyhole size={20} />} title="Write mode required" detail={model.message} tone="warning" /></NetworkCaptureShell>
  }

  /** Opens one network card in the accessible focus overlay. */
  function openFocus(mode: NetworkFocusMode, event: MouseEvent<HTMLButtonElement>): void {
    focusReturnRef.current = event.currentTarget
    setFocusMode(mode)
  }

  /** Closes focus mode and resumes the live chart display. */
  function closeFocus(): void {
    setFocusMode(null)
    setChartPaused(false)
    setPausedSamples(null)
    window.setTimeout(() => focusReturnRef.current?.focus(), 0)
  }

  /** Freezes or resumes the displayed chart samples without stopping packet capture. */
  function toggleChartPause(): void {
    if (chartPaused) {
      setChartPaused(false)
      setPausedSamples(null)
      return
    }
    setPausedSamples(model.samples)
    setChartPaused(true)
  }

  const bandwidthSeries: NetworkChartSeries[] = [{ label: 'Inbound', color: '#a8bd83', key: 'inboundBytesPerSecond' }, { label: 'Outbound', color: '#f09a62', key: 'outboundBytesPerSecond' }]
  const packetSeries: NetworkChartSeries[] = [{ label: 'Inbound', color: '#87aeb6', key: 'inboundPacketsPerSecond' }, { label: 'Outbound', color: '#e6b66f', key: 'outboundPacketsPerSecond' }]
  const hiddenLoopbackCount = rawPeers.filter((peer) => peer.loopback).length

  return <NetworkCaptureShell status={model.status} statusLabel={statusLabel} debugImage={debugImage} live={live}>
    {model.message && ['error', 'permission-denied', 'unsupported', 'capture-error', 'dependency-unavailable'].includes(model.status) && <NetworkCapabilityState icon={<AlertTriangle size={20} />} title={statusLabel} detail={model.message} tone="warning" />}
    {model.status === 'starting' && <div className="network-capture-notice"><LoaderCircle size={16} className="spin" /><span>{model.message || 'Preparing the live capture session.'}</span></div>}
    {model.status === 'max-duration' && <div className="network-capture-notice network-capture-notice-warning"><AlertTriangle size={16} /><span>{model.message || 'The capture reached its safety limit.'}</span></div>}
    <div className="network-view-controls"><label className="network-check-control"><input type="checkbox" checked={includeLoopback} onChange={(event) => setIncludeLoopback(event.target.checked)} /><span>Include localhost</span></label><span>{hiddenLoopbackCount && !includeLoopback ? `${hiddenLoopbackCount} localhost flow${hiddenLoopbackCount === 1 ? '' : 's'} hidden` : 'Logical peer aggregation active'}</span></div>
    <div className="network-stat-grid">
      <NetworkStatCard label="Inbound" value={formatRate(lastSample?.inboundBytesPerSecond || 0)} detail={`${formatBytes(model.inboundBytesTotal)} total`} icon={<ArrowDown size={15} />} tone="moss" />
      <NetworkStatCard label="Outbound" value={formatRate(lastSample?.outboundBytesPerSecond || 0)} detail={`${formatBytes(model.outboundBytesTotal)} total`} icon={<ArrowUp size={15} />} tone="copper" />
      <NetworkStatCard label="Packets" value={formatRate((lastSample?.inboundPacketsPerSecond || 0) + (lastSample?.outboundPacketsPerSecond || 0), true)} detail={`${formatNumber(model.inboundPacketsTotal + model.outboundPacketsTotal)} observed`} icon={<Radio size={15} />} tone="blue" />
      <NetworkStatCard label="Peers" value={formatNumber(peers.length)} detail={hiddenLoopbackCount && !includeLoopback ? `${hiddenLoopbackCount} localhost flows hidden` : 'logical flow summaries'} icon={<CircleDot size={15} />} tone="gold" />
    </div>
    <div className="network-chart-grid">
      <NetworkLineChart title="Observed bandwidth" unit="bytes/sec" points={displaySamples} series={bandwidthSeries} onExpand={(event) => openFocus('bandwidth', event)} />
      <NetworkLineChart title="Packet rate" unit="packets/sec" points={displaySamples} series={packetSeries} onExpand={(event) => openFocus('packets', event)} />
    </div>
    <div className="network-lower-grid">
      <NetworkPeerMap pod={pod} peers={peers} onExpand={(event) => openFocus('map', event)} />
      <NetworkPeerTable peers={peers} onExpand={(event) => openFocus('table', event)} />
    </div>
    <div className="network-capture-footer"><span><Network size={14} /> Header-only capture / no traffic history</span><span>{model.maxDurationSeconds ? `Safety limit ${Math.round(model.maxDurationSeconds / 60)} min` : 'Safety limit enforced by backend'}{debugImage ? ` · ${debugImage}` : ''}</span></div>
    {focusMode && <NetworkFocusOverlay mode={focusMode} paused={chartPaused} onClose={closeFocus} onTogglePause={toggleChartPause}>
      {focusMode === 'bandwidth' && <NetworkLineChart title="Observed bandwidth" unit="bytes/sec" points={displaySamples} series={bandwidthSeries} expanded />}
      {focusMode === 'packets' && <NetworkLineChart title="Packet rate" unit="packets/sec" points={displaySamples} series={packetSeries} expanded />}
      {focusMode === 'map' && <NetworkPeerMap pod={pod} peers={peers} expanded />}
      {focusMode === 'table' && <NetworkPeerTable peers={peers} expanded />}
    </NetworkFocusOverlay>}
  </NetworkCaptureShell>
}

/** Wraps the network view in the existing dense field-console panel treatment. */
function NetworkCaptureShell({ status, statusLabel, debugImage, live, children }: { status: string; statusLabel: string; debugImage?: string; live?: boolean; children: ReactNode }) {
  return <div className="network-panel">
    <div className="network-panel-header"><div><div className="logs-title"><Network size={16} /><strong>Live network</strong><span>Pod-local observation</span></div><p className="network-intro">Traffic visible from this Pod network namespace while this tab is active. The map shows observed peers, not a complete cluster topology.</p></div><span className={`network-status network-status-${status}`}><span />{live ? 'Live' : statusLabel}</span></div>
    {debugImage && <div className="network-image-note"><span>Debug image</span><code>{debugImage}</code></div>}
    {children}
  </div>
}

/** Renders one live network summary card. */
function NetworkStatCard({ label, value, detail, icon, tone }: { label: string; value: string; detail: string; icon: ReactNode; tone: string }) {
  return <div className={`network-stat-card network-stat-card-${tone}`}><div className="network-stat-heading"><span>{icon}</span><span>{label}</span></div><strong>{value}</strong><small>{detail}</small></div>
}

/** Renders a lightweight rolling SVG line chart without a charting dependency. */
function NetworkLineChart({ title, unit, points, series, expanded = false, onExpand }: { title: string; unit: string; points: NetworkChartPoint[]; series: NetworkChartSeries[]; expanded?: boolean; onExpand?: (event: MouseEvent<HTMLButtonElement>) => void }) {
  const [hoverIndex, setHoverIndex] = useState<number | null>(null)
  const width = 640
  const height = 210
  const padding = { top: 22, right: 17, bottom: 27, left: 47 }
  const plotWidth = width - padding.left - padding.right
  const plotHeight = height - padding.top - padding.bottom
  const values = points.flatMap((point) => series.map((item) => Number(point[item.key]) || 0))
  const maximum = Math.max(...values, 1)
  const hasTraffic = values.some((value) => value > 0)
  const linePoints = (key: keyof NetworkChartPoint): string => points.map((point, index) => {
    const x = padding.left + (points.length <= 1 ? plotWidth / 2 : (index / (points.length - 1)) * plotWidth)
    const y = padding.top + plotHeight - ((Number(point[key]) || 0) / maximum) * plotHeight
    return `${x.toFixed(2)},${y.toFixed(2)}`
  }).join(' ')
  const hoveredPoint = hoverIndex === null ? undefined : points[hoverIndex]
  const hoveredX = hoverIndex === null || points.length <= 1 ? padding.left + plotWidth / 2 : padding.left + (hoverIndex / (points.length - 1)) * plotWidth

  /** Selects the nearest sample under the pointer for the chart tooltip. */
  function updateHover(event: MouseEvent<SVGSVGElement>): void {
    if (!points.length) return
    const bounds = event.currentTarget.getBoundingClientRect()
    const svgX = ((event.clientX - bounds.left) / bounds.width) * width
    const ratio = Math.max(0, Math.min(1, (svgX - padding.left) / plotWidth))
    setHoverIndex(points.length <= 1 ? 0 : Math.round(ratio * (points.length - 1)))
  }

  return <section className={`network-chart-card${expanded ? ' network-focus-card' : ''}`}><div className="network-chart-heading">{expanded ? <div className="network-focus-card-meta"><span>{points.length ? hasTraffic ? `${unit} / rolling 60 seconds` : 'No non-loopback traffic / rolling 60 seconds' : `${unit} / waiting for samples`}</span></div> : <div><strong>{title}</strong><span>{points.length ? hasTraffic ? `${unit} / rolling 60 seconds` : 'No non-loopback traffic / rolling 60 seconds' : `${unit} / waiting for samples`}</span></div>}<div className="network-chart-heading-actions"><div className="network-chart-legend">{series.map((item) => <span key={item.label}><i style={{ background: item.color }} />{item.label}</span>)}</div>{onExpand && <button type="button" className="network-card-action" aria-label={`Expand ${title} chart`} title={`Expand ${title} chart`} onClick={onExpand}><Maximize2 size={14} /></button>}</div></div><div className="network-chart-canvas">{points.length ? <><svg className="network-chart-svg" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" role="img" aria-label={`${title}, rolling 60 second live chart`} onMouseMove={updateHover} onMouseLeave={() => setHoverIndex(null)}><g className="network-chart-grid-lines"><line x1={padding.left} x2={width - padding.right} y1={padding.top} y2={padding.top} /><line x1={padding.left} x2={width - padding.right} y1={padding.top + plotHeight / 2} y2={padding.top + plotHeight / 2} /><line x1={padding.left} x2={width - padding.right} y1={padding.top + plotHeight} y2={padding.top + plotHeight} /></g>{series.map((item) => <polyline key={item.label} points={linePoints(item.key)} fill="none" stroke={item.color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />)}{hoveredPoint && <><line className="network-chart-crosshair" x1={hoveredX} x2={hoveredX} y1={padding.top} y2={padding.top + plotHeight} />{series.map((item) => { const value = Number(hoveredPoint[item.key]) || 0; const y = padding.top + plotHeight - (value / maximum) * plotHeight; return <circle key={item.label} cx={hoveredX} cy={y} r="3" fill={item.color} stroke="#11130f" strokeWidth="1.5" /> })}</>}<text x={padding.left - 8} y={padding.top + 4} textAnchor="end">{formatCompactNumber(maximum)}</text><text x={padding.left - 8} y={padding.top + plotHeight / 2 + 4} textAnchor="end">{formatCompactNumber(maximum / 2)}</text><text x={padding.left - 8} y={padding.top + plotHeight + 4} textAnchor="end">0</text><text className="network-chart-time-label" x={padding.left} y={height - 5} textAnchor="start">-60s</text><text className="network-chart-time-label" x={padding.left + plotWidth / 2} y={height - 5} textAnchor="middle">-30s</text><text className="network-chart-time-label" x={width - padding.right} y={height - 5} textAnchor="end">now</text></svg>{hoveredPoint && <div className="network-chart-tooltip" style={{ left: `${(hoveredX / width) * 100}%` }}><strong>{formatChartTime(hoveredPoint.at)}</strong>{series.map((item) => <span key={item.label}><i style={{ background: item.color }} />{item.label} {formatChartValue(Number(hoveredPoint[item.key]) || 0, unit)}</span>)}</div>}</> : <div className="network-chart-empty"><Radio size={17} /><strong>No samples received yet</strong><span>Counter samples appear once the capture command is running.</span></div>}</div></section>
}

/** Renders the selected Pod and its highest-volume observed peers as an SVG network map. */
function NetworkPeerMap({ pod, peers, expanded = false, onExpand }: { pod: string; peers: NetworkPeerRow[]; expanded?: boolean; onExpand?: (event: MouseEvent<HTMLButtonElement>) => void }) {
  const nodes = Array.from(new Map(peers.map((peer) => [peer.peerLabel, peer])).values()).slice(0, expanded ? 30 : 10)
  const center = { x: 50, y: 50 }
  return <section className={`network-map-card${expanded ? ' network-focus-card' : ''}`}><div className="network-section-heading">{expanded ? <div className="network-focus-card-meta"><span>local Pod view / top 30 logical peers</span><code className="network-map-pod-identity" title={pod}>{pod}</code></div> : <div><strong>Observed peer map</strong><span>local Pod view / top 10 logical peers</span></div>}<div className="network-section-actions"><Network size={15} />{onExpand && <button type="button" className="network-card-action" aria-label="Expand observed peer map" title="Expand observed peer map" onClick={onExpand}><Maximize2 size={14} /></button>}</div></div>{nodes.length ? <svg className="network-map-canvas" viewBox="0 0 100 100" role="img" aria-label={`Observed peers for Pod ${pod}`}>{nodes.map((peer, index) => { const angle = (Math.PI * 2 * index) / nodes.length - Math.PI / 2; const node = { x: 50 + Math.cos(angle) * 34, y: 50 + Math.sin(angle) * 34 }; return <g key={peer.key}><title>{peer.peerLabel}</title><line className={`network-map-edge network-map-edge-${peer.direction}`} x1={center.x} y1={center.y} x2={node.x} y2={node.y} /><circle className={`network-map-node network-map-node-${peer.direction}`} cx={node.x} cy={node.y} r={expanded ? '2.7' : '4'} /><text x={node.x} y={node.y + (node.y < 50 ? -7 : 10)} textAnchor="middle">{shortNetworkLabel(peer.peerLabel)}</text></g> })}<circle className="network-map-center" cx={center.x} cy={center.y} r="7" /><text className="network-map-center-label" x={center.x} y={center.y + 1} textAnchor="middle">Pod</text><text className="network-map-pod-label" x={center.x} y="65" textAnchor="middle">{shortNetworkLabel(pod)}</text></svg> : <div className="network-map-empty"><CircleDot size={19} /><strong>No peer packets yet</strong><span>Start traffic to or from this Pod while the capture is live.</span></div>}</section>
}

/** Renders the bounded peer table that complements the local SVG map. */
function NetworkPeerTable({ peers, expanded = false, onExpand }: { peers: NetworkPeerRow[]; expanded?: boolean; onExpand?: (event: MouseEvent<HTMLButtonElement>) => void }) {
  const [query, setQuery] = useState('')
  const [direction, setDirection] = useState<'all' | 'inbound' | 'outbound' | 'unknown'>('all')
  const filteredPeers = expanded ? peers.filter((peer) => {
    const matchesDirection = direction === 'all' || peer.direction === direction
    const needle = query.trim().toLowerCase()
    return matchesDirection && (!needle || `${peer.peerLabel} ${peer.peerAddress} ${peer.protocol}`.toLowerCase().includes(needle))
  }) : peers
  const visiblePeers = expanded ? filteredPeers : filteredPeers.slice(0, 12)
  return <section className={`network-peer-card${expanded ? ' network-focus-card' : ''}`}><div className="network-section-heading">{expanded ? <div className="network-focus-card-meta"><span>logical peer summaries / current session</span></div> : <div><strong>Top observed flows</strong><span>logical peer summaries / current session</span></div>}<div className="network-section-actions"><span>{peers.length} tracked</span>{onExpand && <button type="button" className="network-card-action" aria-label="Expand observed flows" title="Expand observed flows" onClick={onExpand}><Maximize2 size={14} /></button>}</div></div>{expanded && <div className="network-peer-filters"><label><Search size={13} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search peers" aria-label="Search observed peers" /></label><label className="network-direction-select"><select value={direction} onChange={(event) => setDirection(event.target.value as typeof direction)} aria-label="Filter observed peer direction"><option value="all">All directions</option><option value="inbound">Inbound</option><option value="outbound">Outbound</option><option value="unknown">Unknown</option></select><ChevronDown size={14} /></label></div>}{visiblePeers.length ? <div className="network-peer-table-wrap"><table className="network-peer-table"><thead><tr><th>Direction</th><th>Peer</th><th>Protocol</th><th>Packets</th><th>Payload</th></tr></thead><tbody>{visiblePeers.map((peer) => <tr key={peer.key}><td><span className={`network-direction network-direction-${peer.direction}`}>{peer.direction}</span></td><td><strong>{peer.peerLabel}</strong><small>{peer.peerAddress !== peer.peerLabel ? peer.peerAddress : `${peer.source} → ${peer.destination}`}</small></td><td><code>{peer.protocol}{peer.destinationPort ? `/${peer.destinationPort}` : ''}</code></td><td>{formatNumber(peer.packets)}</td><td>{formatBytes(peer.payloadBytes)}</td></tr>)}</tbody></table></div> : <div className="network-table-empty">{expanded && (query || direction !== 'all') ? 'No flows match the current filter.' : 'Peer summaries appear after tcpdump observes packets.'}</div>}</section>
}

/** Renders one keyboard-safe in-app focus overlay for a network analysis card. */
function NetworkFocusOverlay({ mode, paused, onClose, onTogglePause, children }: { mode: NetworkFocusMode; paused: boolean; onClose: () => void; onTogglePause: () => void; children: ReactNode }) {
  const closeRef = useRef<HTMLButtonElement | null>(null)
  const title = mode === 'bandwidth' ? 'Observed bandwidth' : mode === 'packets' ? 'Packet rate' : mode === 'map' ? 'Observed peer map' : 'Top observed flows'

  useEffect(() => {
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    closeRef.current?.focus()
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => {
      document.body.style.overflow = previousOverflow
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [])

  return createPortal(<div className="network-focus-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}><section className="network-focus-dialog" role="dialog" aria-modal="true" aria-labelledby="network-focus-title"><div className="network-focus-dialog-header"><div><span className="panel-eyebrow">Focused live view</span><h2 id="network-focus-title">{title}</h2></div><div className="network-focus-dialog-actions">{(mode === 'bandwidth' || mode === 'packets') && <button type="button" className="secondary-button" onClick={onTogglePause}>{paused ? <Play size={14} /> : <Pause size={14} />}{paused ? 'Resume live' : 'Pause view'}</button>}<button ref={closeRef} type="button" className="icon-button" aria-label="Close focused network view" title="Close focused network view" onClick={onClose}><X size={17} /></button></div></div><div className="network-focus-dialog-body">{children}</div></section></div>, document.body)
}

/** Consumes newline-delimited server-sent event frames from the live capture stream. */
async function consumeNetworkEventStream(response: Response, signal: AbortSignal, onEvent: (event: NetworkCaptureEvent) => void): Promise<void> {
  if (!response.body) throw new Error('The network capture stream returned no body.')
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const chunk = await reader.read()
    if (chunk.done) break
    if (signal.aborted) return
    buffer += decoder.decode(chunk.value, { stream: true })
    const frames = buffer.split('\n\n')
    buffer = frames.pop() || ''
    frames.forEach((frame) => {
      const event = parseNetworkEventFrame(frame)
      if (event) onEvent(event)
    })
  }
  buffer += decoder.decode()
  const finalEvent = parseNetworkEventFrame(buffer)
  if (finalEvent) onEvent(finalEvent)
}

/** Returns the Pod IP addresses used to classify observed flow direction. */
function podNetworkIPs(object: ResourceObject): string[] {
  const status = object.status && typeof object.status === 'object' && !Array.isArray(object.status) ? object.status as Record<string, unknown> : {}
  const ips = [typeof status.podIP === 'string' ? status.podIP : '']
  if (Array.isArray(status.podIPs)) status.podIPs.forEach((item) => { if (item && typeof item === 'object' && !Array.isArray(item) && typeof (item as Record<string, unknown>).ip === 'string') ips.push((item as Record<string, string>).ip) })
  return Array.from(new Set(ips.filter(Boolean)))
}

/** Converts an unknown capture failure into a stable display state. */
function networkErrorState(error: unknown): string {
  const status = typeof error === 'object' && error && 'status' in error ? Number((error as { status?: number }).status) : 0
  if (status === 403) return 'permission-denied'
  if (status === 501) return 'unsupported'
  return 'error'
}

/** Returns the operator-facing label for one capture state. */
function networkStatusLabel(status: string): string {
  switch (status) {
    case 'starting': return 'Starting'
    case 'capturing': return 'Live'
    case 'max-duration': return 'Safety limit'
    case 'closed': return 'Stopped'
    case 'write-required': return 'Write mode required'
    case 'permission-denied': return 'Permission denied'
    case 'unsupported': return 'Unavailable'
    case 'capture-error': return 'Capture error'
    default: return 'Unavailable'
  }
}

/** Renders a capability or permission state with direct remediation guidance. */
function NetworkCapabilityState({ icon, title, detail, tone }: { icon: ReactNode; title: string; detail: string; tone: string }) {
  return <div className={`network-capability-state network-capability-state-${tone}`}><span>{icon}</span><div><strong>{title}</strong><span>{detail}</span></div></div>
}

/** Formats a live byte rate using compact operator-friendly units. */
function formatRate(value: number, packets = false): string {
  if (packets) return `${formatCompactNumber(value)}/s`
  return `${formatBytes(value)}/s`
}

/** Formats a byte count using binary units. */
function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`
}

/** Formats a large count without introducing decimal noise into dense cards. */
function formatNumber(value: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(Number.isFinite(value) ? value : 0)
}

/** Formats chart axis values in compact decimal notation. */
function formatCompactNumber(value: number): string {
  if (value >= 1000000) return `${(value / 1000000).toFixed(1)}M`
  if (value >= 1000) return `${(value / 1000).toFixed(1)}k`
  return Math.round(value).toString()
}

/** Formats one chart sample timestamp for hover inspection. */
function formatChartTime(value: number): string {
  if (!Number.isFinite(value)) return 'unknown time'
  return `${new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(value))} UTC`
}

/** Formats one chart tooltip value using its displayed unit. */
function formatChartValue(value: number, unit: string): string {
  return unit === 'bytes/sec' ? `${formatBytes(value)}/s` : `${formatCompactNumber(value)}/s`
}

/** Shortens SVG labels while preserving the identifying end of a long address or name. */
function shortNetworkLabel(value: string): string {
  if (value.length <= 18) return value
  return `${value.slice(0, 7)}…${value.slice(-8)}`
}
