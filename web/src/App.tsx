import {
  Activity,
  AlertTriangle,
  ArrowUpRight,
  Box,
  Bug,
  Check,
  ChevronDown,
  CircleDot,
  Clock3,
  Command,
  Database,
  Download,
  FileCode2,
  Gauge,
  HardDrive,
  Layers3,
  ListFilter,
  LoaderCircle,
  LockKeyhole,
  Menu,
  MoreHorizontal,
  Network,
  RefreshCw,
  Search,
  Server,
  Settings2,
  ShieldCheck,
  TerminalSquare,
  Upload,
  X,
  Zap,
} from 'lucide-react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { stringify } from 'yaml'
import {
  applyResource,
  fetchContexts,
  fetchDiscovery,
  fetchOverview,
  fetchResource,
  fetchResourceEvents,
  fetchResources,
  downloadPodFile,
  launchEphemeralContainer,
  logsUrl,
  portForwardUrl,
  uploadPodFile,
  resourceEventsWatchUrl,
  terminalUrl,
  watchUrl,
} from './api'
import type {
  ClusterOverview,
  ContextSummary,
  ContextsResponse,
  EphemeralContainerRequest,
  EventSummary,
  PortForwardMessage,
  ResourceDefinition,
  ResourceListResponse,
  ResourceOwnerReference,
  ResourceObject,
  ResourceSummary,
  RouteState,
  TerminalMessage,
  MetricsOverview,
  MetricsState,
  NodeMetric,
  PodMetric,
} from './types'

const commonDefinitions: ResourceDefinition[] = [
  { group: 'apps', version: 'v1', resource: 'deployments', kind: 'Deployment', namespaced: true, verbs: [] },
  { group: 'apps', version: 'v1', resource: 'statefulsets', kind: 'StatefulSet', namespaced: true, verbs: [] },
  { group: 'apps', version: 'v1', resource: 'daemonsets', kind: 'DaemonSet', namespaced: true, verbs: [] },
  { group: 'batch', version: 'v1', resource: 'jobs', kind: 'Job', namespaced: true, verbs: [] },
  { group: 'batch', version: 'v1', resource: 'cronjobs', kind: 'CronJob', namespaced: true, verbs: [] },
  { group: '', version: 'v1', resource: 'pods', kind: 'Pod', namespaced: true, verbs: [] },
  { group: '', version: 'v1', resource: 'services', kind: 'Service', namespaced: true, verbs: [] },
  { group: '', version: 'v1', resource: 'configmaps', kind: 'ConfigMap', namespaced: true, verbs: [] },
  { group: '', version: 'v1', resource: 'secrets', kind: 'Secret', namespaced: true, verbs: [] },
  { group: '', version: 'v1', resource: 'persistentvolumeclaims', kind: 'PersistentVolumeClaim', namespaced: true, verbs: [] },
  { group: '', version: 'v1', resource: 'nodes', kind: 'Node', namespaced: false, verbs: [] },
  { group: '', version: 'v1', resource: 'namespaces', kind: 'Namespace', namespaced: false, verbs: [] },
  { group: '', version: 'v1', resource: 'events', kind: 'Event', namespaced: true, verbs: [] },
]

const navGroups = [
  {
    label: 'Observe',
    items: [
      { label: 'Cluster overview', resource: undefined, icon: Gauge },
      { label: 'Pods', resource: 'pods', icon: CircleDot },
      { label: 'Events', resource: 'events', icon: Activity },
    ],
  },
  {
    label: 'Workloads',
    items: [
      { label: 'Deployments', resource: 'deployments', icon: Layers3 },
      { label: 'StatefulSets', resource: 'statefulsets', icon: Database },
      { label: 'DaemonSets', resource: 'daemonsets', icon: Zap },
      { label: 'Jobs & schedules', resource: 'jobs', icon: Clock3 },
    ],
  },
  {
    label: 'Configure',
    items: [
      { label: 'Services', resource: 'services', icon: Network },
      { label: 'ConfigMaps', resource: 'configmaps', icon: FileCode2 },
      { label: 'Secrets', resource: 'secrets', icon: LockKeyhole },
      { label: 'Storage claims', resource: 'persistentvolumeclaims', icon: HardDrive },
    ],
  },
]

/** Provides the top-level KubeHaggis application shell. */
export default function App() {
  const [route, setRoute] = useState<RouteState>(() => readRoute())
  const [contexts, setContexts] = useState<ContextsResponse | null>(null)
  const [selectedContext, setSelectedContext] = useState('')
  const [selectedNamespace, setSelectedNamespace] = useState('all')
  const [definitions, setDefinitions] = useState<ResourceDefinition[]>(commonDefinitions)
  const [writeEnabled, setWriteEnabled] = useState(false)
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)

  useEffect(() => {
    let active = true
    fetchContexts()
      .then((response) => {
        if (!active) return
        setContexts(response)
        const search = new URLSearchParams(window.location.search)
        const fromUrl = search.get('context')
        const context = fromUrl || response.currentContext
        setSelectedContext(context)
      })
      .catch((error: Error) => {
        if (!active) return
        setContexts({ currentContext: '', contexts: [], error: error.message })
      })
    return () => {
      active = false
    }
  }, [])

  useEffect(() => {
    if (!selectedContext) return
    const routeNamespace = new URLSearchParams(window.location.search).get('namespace')
    setSelectedNamespace(routeNamespace || readNamespace(selectedContext))
    setWriteEnabled(false)
    fetchDiscovery(selectedContext).then(setDefinitions).catch(() => setDefinitions(commonDefinitions))
  }, [selectedContext])

  /** Navigates to a route while preserving the selected context and namespace. */
  function navigate(path: string, resource?: string, name?: string, resourceNamespace?: string) {
    const nextPath = path || '/'
    const params = new URLSearchParams()
    if (selectedContext) params.set('context', selectedContext)
    params.set('namespace', resourceNamespace || selectedNamespace)
    window.history.pushState({}, '', `${nextPath}?${params.toString()}`)
    setRoute(readRoute())
    setMobileMenuOpen(false)
  }

  /** Changes the active kubeconfig context and starts it in read-only mode. */
  function changeContext(context: string) {
    setSelectedContext(context)
    const namespace = readNamespace(context)
    setSelectedNamespace(namespace)
    const params = new URLSearchParams(window.location.search)
    params.set('context', context)
    params.set('namespace', namespace)
    window.history.replaceState({}, '', `${window.location.pathname}?${params.toString()}`)
  }

  /** Changes and persists the namespace scope for the active context. */
  function changeNamespace(namespace: string) {
    setSelectedNamespace(namespace)
    if (selectedContext) window.localStorage.setItem(namespaceKey(selectedContext), namespace)
    const params = new URLSearchParams(window.location.search)
    params.set('namespace', namespace)
    window.history.replaceState({}, '', `${window.location.pathname}?${params.toString()}`)
  }

  useEffect(() => {
    /** Updates the React route when browser history navigation occurs. */
    function handlePopState() {
      setRoute(readRoute())
    }
    window.addEventListener('popstate', handlePopState)
    return () => window.removeEventListener('popstate', handlePopState)
  }, [])

  const activeDefinition = route.resource ? resolveDefinition(route.resource, definitions) : undefined
  const activeContext = contexts?.contexts.find((item) => item.name === selectedContext)
  const validResourceName = route.name && route.name !== 'undefined' && route.name !== 'null' ? route.name : undefined
  const content = !contexts || (contexts.contexts.length > 0 && !selectedContext) ? (
    <LoadingState label="Reading local kubeconfig" />
  ) : contexts.error || contexts.contexts.length === 0 ? (
    <SetupState error={contexts.error} />
  ) : validResourceName && activeDefinition ? (
    <ResourceDetailPage
      context={selectedContext}
      namespace={route.namespace ?? selectedNamespace}
      definition={activeDefinition}
      name={validResourceName}
      definitions={definitions}
      writeEnabled={writeEnabled}
      onBack={() => navigate(`/resources/${activeDefinition.resource}`, activeDefinition.resource)}
      onOpenEvent={(event) => navigate(`/resources/events/${encodeURIComponent(event.name)}`, 'events', event.name, event.namespace || undefined)}
      onOpenResource={(definition, resourceName, resourceNamespace) => navigate(`/resources/${definition.resource}/${encodeURIComponent(resourceName)}`, definition.resource, resourceName, resourceNamespace)}
    />
  ) : route.resource && activeDefinition ? (
    <ResourceListPage
      context={selectedContext}
      namespace={selectedNamespace}
      definition={activeDefinition}
      writeEnabled={writeEnabled}
      onOpen={(name, itemNamespace) => navigate(`/resources/${activeDefinition.resource}/${encodeURIComponent(name)}`, activeDefinition.resource, name, activeDefinition.namespaced && selectedNamespace === 'all' ? itemNamespace : undefined)}
      onCreate={() => navigate(`/resources/${activeDefinition.resource}/new`, activeDefinition.resource, 'new')}
    />
  ) : (
    <OverviewPage context={selectedContext} contexts={contexts} activeContext={activeContext} />
  )

  return (
    <div className="app-shell">
      <aside className={`sidebar ${mobileMenuOpen ? 'sidebar-open' : ''}`}>
        <div className="brand-lockup">
          <img src="/haggis.svg" alt="" className="brand-mark" />
          <div>
            <span className="brand-name">KubeHaggis</span>
            <span className="brand-subtitle">cluster field console</span>
          </div>
          <button className="icon-button sidebar-close" onClick={() => setMobileMenuOpen(false)} aria-label="Close navigation">
            <X size={18} />
          </button>
        </div>
        <div className="sidebar-rule" />
        <nav className="side-nav" aria-label="Primary navigation">
          {navGroups.map((group) => (
            <div className="nav-group" key={group.label}>
              <span className="nav-label">{group.label}</span>
              {group.items.map((item) => {
                const Icon = item.icon
                const isActive = item.resource ? route.resource === item.resource : !route.resource
                const destination = item.resource ? `/resources/${item.resource}` : '/'
                return (
                  <a
                    className={`nav-item ${isActive ? 'nav-item-active' : ''}`}
                    key={item.label}
                    href={routeHref(destination, selectedContext, selectedNamespace)}
                    onClick={() => setMobileMenuOpen(false)}
                  >
                    <Icon size={16} strokeWidth={1.8} />
                    <span>{item.label}</span>
                    {isActive && <span className="nav-active-dot" />}
                  </a>
                )
              })}
            </div>
          ))}
        </nav>
        <div className="sidebar-footer">
          <div className="footer-signal"><span className="signal-dot" /> Local session</div>
          <span className="footer-version">v0.1 / native API</span>
        </div>
      </aside>

      <main className="main-shell">
        <header className="topbar">
          <button className="icon-button menu-toggle" onClick={() => setMobileMenuOpen(true)} aria-label="Open navigation">
            <Menu size={20} />
          </button>
          <div className="crumbs">
            <span className="crumb-muted">KubeHaggis</span>
            <span className="crumb-slash">/</span>
            <span>{route.resource ? titleCase(route.resource) : 'Cluster overview'}</span>
          </div>
          <div className="topbar-tools">
            <ContextPicker contexts={contexts?.contexts ?? []} value={selectedContext} onChange={changeContext} />
            <NamespacePicker context={selectedContext} value={selectedNamespace} onChange={changeNamespace} />
            <button
              className={`write-switch ${writeEnabled ? 'write-switch-on' : ''}`}
              onClick={() => setWriteEnabled((enabled) => !enabled)}
              disabled={!selectedContext}
            >
              <span className="write-switch-light" />
              {writeEnabled ? 'Write mode on' : 'Read-only'}
            </button>
            <button className="icon-button" aria-label="Settings">
              <Settings2 size={18} />
            </button>
          </div>
        </header>
        <div className="page-scroll">{content}</div>
      </main>
    </div>
  )
}

/** Renders the cluster overview and its health summary panels. */
function OverviewPage({ context, contexts, activeContext }: { context: string; contexts: ContextsResponse | null; activeContext?: ContextSummary }) {
  const [overview, setOverview] = useState<ClusterOverview | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(Boolean(context))

  useEffect(() => {
    if (!context) {
      setLoading(false)
      return
    }
    let active = true
    const load = () => {
      setLoading(true)
      fetchOverview(context)
        .then((response) => {
          if (!active) return
          setOverview(response)
          setError('')
        })
        .catch((loadError: Error) => {
          if (!active) return
          setError(loadError.message)
        })
        .finally(() => active && setLoading(false))
    }
    load()
    const interval = window.setInterval(load, 30000)
    return () => {
      active = false
      window.clearInterval(interval)
    }
  }, [context])

  if (!contexts) return <LoadingState label="Reading local kubeconfig" />
  if (contexts.error || contexts.contexts.length === 0) {
    return <SetupState error={contexts?.error} />
  }
  if (loading && !overview) return <LoadingState label="Reading cluster signal and native metrics" />
  if (error && !overview) return <ConnectionState context={activeContext} error={error} />
  if (!overview) return <EmptyState title="No cluster selected" detail="Choose a context above to open its overview." />

  const workloadHealth = overview.workloads.deployments + overview.workloads.statefulSets + overview.workloads.daemonSets
  const workloadReady = overview.workloads.healthyDeployments + overview.workloads.healthyStatefulSets + overview.workloads.healthyDaemonSets
  const warningCount = overview.warnings.length

  return (
    <div className="dashboard-page">
      <section className="dashboard-intro">
        <div>
          <div className="eyebrow"><span className="eyebrow-mark" /> Current cluster / live watch</div>
          <h1>Cluster overview</h1>
          <p className="intro-copy">A quiet read on the systems running under <strong>{overview.context.cluster || overview.context.name}</strong>.</p>
        </div>
        <div className="context-stamp">
          <span className="stamp-label">API endpoint</span>
          <code>{shortServer(overview.context.server)}</code>
          <span className="stamp-status"><span className="signal-dot signal-dot-good" /> Connected</span>
        </div>
      </section>

      <section className="stat-grid">
        <MetricCard label="Nodes ready" value={`${overview.nodes.ready}/${overview.nodes.total}`} detail={overview.nodes.pressure ? `${overview.nodes.pressure} under pressure` : `${overview.nodes.cordoned} cordoned`} tone={overview.nodes.notReady ? 'warning' : 'good'} icon={<Server size={17} />} />
        <MetricCard label="Pods running" value={`${overview.pods.running}`} detail={`${overview.pods.pending} pending / ${overview.pods.failed} failed`} tone={overview.pods.failed || overview.pods.pending ? 'warning' : 'good'} icon={<CircleDot size={17} />} />
        <MetricCard label="Workload health" value={workloadHealth ? `${workloadReady}/${workloadHealth}` : '—'} detail={`${overview.workloads.jobs} jobs / ${overview.workloads.cronJobs} schedules`} tone={workloadHealth && workloadReady < workloadHealth ? 'warning' : 'good'} icon={<Layers3 size={17} />} />
        <MetricCard label="Storage claims" value={`${overview.storage.bound}/${overview.storage.total}`} detail={`${overview.storage.pending} pending / ${overview.storage.lost} lost`} tone={overview.storage.pending || overview.storage.lost ? 'warning' : 'good'} icon={<HardDrive size={17} />} />
      </section>

      <section className="dashboard-grid">
        <div className="panel signal-panel">
          <PanelHeading eyebrow="Signal check" title="What needs your eye" action={warningCount ? `${warningCount} warnings` : 'No warnings'} tone={warningCount ? 'warning' : 'good'} />
          {overview.warnings.length ? <WarningList warnings={overview.warnings} /> : <QuietState icon={<ShieldCheck size={22} />} title="Nothing shouting" detail="No recent Warning Events were reported." />}
        </div>
        <div className="panel posture-panel">
          <PanelHeading eyebrow="Cluster posture" title="Capacity & coverage" action={metricsStateLabel(overview.metricsAvailable, overview.metrics.state)} tone={metricsStateTone(overview.metricsAvailable, overview.metrics.state)} />
          <div className="posture-stack">
            <PostureRow icon={<Layers3 size={16} />} label="Namespaces" value={String(overview.namespaces)} note="scoped workspaces" />
            <PostureRow icon={<Activity size={16} />} label="Restarts" value={String(overview.pods.restarting)} note="Pods with restarts" tone={overview.pods.restarting ? 'warning' : 'good'} />
            <PostureRow icon={<HardDrive size={16} />} label="Bound claims" value={String(overview.storage.bound)} note="PersistentVolumeClaims" />
          </div>
          <div className="panel-note"><Gauge size={14} /> Native usage stays read-only and follows Kubernetes RBAC.</div>
        </div>
      </section>

      <MetricsPanel metrics={overview.metrics} metricsAvailable={overview.metricsAvailable} loading={loading} />

      <section className="dashboard-footer-line">
        <span><RefreshCw size={13} /> Refreshed {relativeTime(overview.updatedAt)}</span>
        <span><Command size={13} /> Context <strong>{overview.context.name}</strong></span>
        <span><LockKeyhole size={13} /> Dashboard is read-only</span>
      </section>
    </div>
  )
}

/** Renders native node and Pod usage charts with explicit source states. */
function MetricsPanel({ metrics, metricsAvailable, loading }: { metrics: MetricsOverview; metricsAvailable: boolean; loading: boolean }) {
  const hasNodeSamples = metrics.nodes.length > 0
  const hasPodSamples = metrics.pods.length > 0
  const hasSamples = hasNodeSamples || hasPodSamples
  const displayState: MetricsDisplayState = loading && !hasSamples ? 'loading' : metrics.state

  return (
    <section className="metrics-panel panel">
      <div className="metrics-panel-heading">
        <div>
          <span className="panel-eyebrow">Native telemetry / metrics.k8s.io</span>
          <h2>CPU &amp; memory field read</h2>
          <p>Usage is sampled from the Kubernetes Metrics API. Nodes show capacity percentages; Pod rows combine all containers.</p>
        </div>
        <span className={`panel-action panel-action-${metricsStateTone(metricsAvailable, displayState)}`}><span />{metricsStateLabel(metricsAvailable, displayState)}</span>
      </div>
      {loading && hasSamples && <div className="metrics-refresh-strip"><LoaderCircle size={14} className="spin" /> Refreshing native metrics; the last sample remains visible.</div>}
      {!hasSamples ? <MetricStateNotice state={displayState} message={displayState === 'loading' ? undefined : metrics.message} /> : null}
      {hasSamples ? (
        <div className="metrics-chart-grid">
          <NodeUsageChart state={metrics.nodeState} message={metrics.nodeMessage} count={metrics.nodeCount} items={metrics.nodes} />
          <PodUsageChart state={metrics.podState} message={metrics.podMessage} count={metrics.podCount} items={metrics.pods} />
        </div>
      ) : null}
      {hasMetricTimestamp(metrics.updatedAt) && <div className="metrics-panel-footer"><span><Activity size={13} /> Latest sample {relativeTime(metrics.updatedAt)}</span><span>{metrics.message || 'Native metrics are read through the local Go backend.'}</span></div>}
    </section>
  )
}

/** Identifies the additional loading state used while the overview is being refreshed. */
type MetricsDisplayState = MetricsState | 'loading'

/** Renders a node usage chart with CPU and memory capacity bars. */
function NodeUsageChart({ state, message, count, items }: { state: MetricsState; message?: string; count: number; items: NodeMetric[] }) {
  const maxCpu = Math.max(...items.map((item) => item.usage.cpuCores), 0)
  const maxMemory = Math.max(...items.map((item) => item.usage.memoryBytes), 0)
  const showNotice = state !== 'available' && state !== 'stale'
  return (
    <article className="metrics-chart">
      <div className="metrics-chart-heading">
        <div className="metrics-chart-title"><span className="metrics-chart-icon metrics-chart-icon-copper"><Gauge size={16} /></span><div><h3>Nodes</h3><span>{count} samples / top {items.length}</span></div></div>
        <span className={`metrics-source-chip metrics-source-${state}`}>{metricsSourceLabel(state)}</span>
      </div>
      {showNotice || !items.length ? <MetricStateNotice state={state} message={message} compact /> : null}
      {state === 'stale' && items.length ? <div className="metrics-stale-note"><AlertTriangle size={14} />{message || 'Node samples may be stale.'}</div> : null}
      {!showNotice && items.length ? <div className="metrics-chart-list">{items.map((item) => <NodeUsageRow key={item.name} item={item} maxCpu={maxCpu} maxMemory={maxMemory} />)}</div> : null}
    </article>
  )
}

/** Renders a Pod usage chart with relative CPU and memory bars. */
function PodUsageChart({ state, message, count, items }: { state: MetricsState; message?: string; count: number; items: PodMetric[] }) {
  const maxCpu = Math.max(...items.map((item) => item.usage.cpuCores), 0)
  const maxMemory = Math.max(...items.map((item) => item.usage.memoryBytes), 0)
  const showNotice = state !== 'available' && state !== 'stale'
  return (
    <article className="metrics-chart">
      <div className="metrics-chart-heading">
        <div className="metrics-chart-title"><span className="metrics-chart-icon metrics-chart-icon-moss"><CircleDot size={16} /></span><div><h3>Pods</h3><span>{count} samples / top {items.length} by CPU</span></div></div>
        <span className={`metrics-source-chip metrics-source-${state}`}>{metricsSourceLabel(state)}</span>
      </div>
      {showNotice || !items.length ? <MetricStateNotice state={state} message={message} compact /> : null}
      {state === 'stale' && items.length ? <div className="metrics-stale-note"><AlertTriangle size={14} />{message || 'Pod samples may be stale.'}</div> : null}
      {!showNotice && items.length ? <div className="metrics-chart-list">{items.map((item) => <PodUsageRow key={`${item.namespace}/${item.name}`} item={item} maxCpu={maxCpu} maxMemory={maxMemory} />)}</div> : null}
    </article>
  )
}

/** Renders one node's CPU and memory utilization bars. */
function NodeUsageRow({ item, maxCpu, maxMemory }: { item: NodeMetric; maxCpu: number; maxMemory: number }) {
  const cpuWidth = usageBarWidth(item.cpuPercent, item.usage.cpuCores, maxCpu)
  const memoryWidth = usageBarWidth(item.memoryPercent, item.usage.memoryBytes, maxMemory)
  return <div className="metrics-row"><div className="metrics-row-label"><strong>{item.name}</strong><span>{formatMetricCPU(item.usage.cpuCores)} CPU / {formatMetricMemory(item.usage.memoryBytes)}</span></div><MetricBar label="CPU" width={cpuWidth} value={item.cpuPercent > 0 ? `${formatMetricPercent(item.cpuPercent)}` : formatMetricCPU(item.usage.cpuCores)} tone="copper" /><MetricBar label="MEM" width={memoryWidth} value={item.memoryPercent > 0 ? `${formatMetricPercent(item.memoryPercent)}` : formatMetricMemory(item.usage.memoryBytes)} tone="moss" /></div>
}

/** Renders one Pod's aggregated CPU and memory bars. */
function PodUsageRow({ item, maxCpu, maxMemory }: { item: PodMetric; maxCpu: number; maxMemory: number }) {
  const namespace = item.namespace ? `${item.namespace}/` : ''
  return <div className="metrics-row"><div className="metrics-row-label"><strong>{namespace}{item.name}</strong><span>{formatMetricCPU(item.usage.cpuCores)} CPU / {formatMetricMemory(item.usage.memoryBytes)}</span></div><MetricBar label="CPU" width={usageBarWidth(0, item.usage.cpuCores, maxCpu)} value={formatMetricCPU(item.usage.cpuCores)} tone="copper" /><MetricBar label="MEM" width={usageBarWidth(0, item.usage.memoryBytes, maxMemory)} value={formatMetricMemory(item.usage.memoryBytes)} tone="moss" /></div>
}

/** Renders one labeled usage bar shared by node and Pod rows. */
function MetricBar({ label, width, value, tone }: { label: string; width: number; value: string; tone: 'copper' | 'moss' }) {
  return <div className="metric-bar-line"><span className="metric-bar-label">{label}</span><span className={`metric-bar-track metric-bar-track-${tone}`}><span style={{ width: `${width}%` }} /></span><code>{value}</code></div>
}

/** Renders an unavailable, loading, empty, denied, stale, or error metrics notice. */
function MetricStateNotice({ state, message, compact = false }: { state: MetricsDisplayState; message?: string; compact?: boolean }) {
  const icon = state === 'loading' ? <LoaderCircle size={17} className="spin" /> : state === 'permission-denied' ? <LockKeyhole size={17} /> : state === 'error' || state === 'stale' ? <AlertTriangle size={17} /> : state === 'empty' ? <Box size={17} /> : <Gauge size={17} />
  return <div className={`metric-state-notice metric-state-${state} ${compact ? 'metric-state-notice-compact' : ''}`}><span className="metric-state-icon">{icon}</span><div><strong>{metricStateTitle(state)}</strong><span>{message || metricStateFallback(state)}</span></div></div>
}

/** Returns the dashboard label for an overall metrics state. */
function metricsStateLabel(metricsAvailable: boolean, state: MetricsDisplayState): string {
  if (state === 'loading') return 'Collecting metrics'
  if (state === 'permission-denied') return 'Metrics access denied'
  if (state === 'error') return 'Metrics read failed'
  if (state === 'stale') return 'Metrics stale'
  if (state === 'empty') return 'No samples yet'
  if (!metricsAvailable || state === 'unavailable') return 'Metrics-server absent'
  return 'Metrics online'
}

/** Returns the panel heading tone for an overall metrics state. */
function metricsStateTone(metricsAvailable: boolean, state: MetricsDisplayState): 'good' | 'warning' | 'neutral' {
  if (state === 'available' && metricsAvailable) return 'good'
  if (state === 'unavailable' || state === 'empty') return 'neutral'
  return 'warning'
}

/** Returns the compact source label shown beside one metrics chart. */
function metricsSourceLabel(state: MetricsState): string {
  if (state === 'permission-denied') return 'RBAC denied'
  if (state === 'unavailable') return 'Not exposed'
  if (state === 'empty') return 'No samples'
  if (state === 'error') return 'Read error'
  if (state === 'stale') return 'Stale sample'
  return 'Native API'
}

/** Returns the human-facing title for an explicit metrics state. */
function metricStateTitle(state: MetricsDisplayState): string {
  if (state === 'loading') return 'Collecting native usage'
  if (state === 'permission-denied') return 'Metrics access is denied'
  if (state === 'unavailable') return 'Metrics-server is not available'
  if (state === 'empty') return 'No usage samples yet'
  if (state === 'error') return 'Metrics could not be read'
  if (state === 'stale') return 'Usage sample is stale'
  return 'Native usage is ready'
}

/** Returns a useful fallback explanation when the backend has no message. */
function metricStateFallback(state: MetricsDisplayState): string {
  if (state === 'loading') return 'Reading node and Pod usage from metrics.k8s.io.'
  if (state === 'permission-denied') return 'Kubernetes RBAC does not allow this identity to list metrics.k8s.io resources.'
  if (state === 'unavailable') return 'Install or register metrics-server to expose metrics.k8s.io/v1beta1.'
  if (state === 'empty') return 'The Metrics API answered, but it returned no node or Pod samples.'
  if (state === 'error') return 'The native Metrics API returned an error; the rest of the overview is still available.'
  if (state === 'stale') return 'The latest sample is older than the dashboard freshness window.'
  return 'Usage is available through the native Kubernetes Metrics API.'
}

/** Calculates a bounded bar width using capacity percentages or visible-row scaling. */
function usageBarWidth(capacityPercent: number, value: number, maximum: number): number {
  if (capacityPercent > 0) return Math.min(100, Math.max(0, capacityPercent))
  if (maximum <= 0) return 0
  return Math.min(100, Math.max(0, value / maximum * 100))
}

/** Formats a normalized CPU value for a dense operator row. */
function formatMetricCPU(cores: number): string {
  if (cores < 1) return `${Math.round(cores * 1000)}m`
  return `${cores.toFixed(2).replace(/0+$/, '').replace(/\.$/, '')} cores`
}

/** Formats a normalized memory value using binary Kubernetes units. */
function formatMetricMemory(bytes: number): string {
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KiB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1).replace(/\.0$/, '')} MiB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1).replace(/\.0$/, '')} GiB`
}

/** Formats a utilization percentage without noisy decimal places. */
function formatMetricPercent(value: number): string {
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1).replace(/\.0$/, '')}%`
}

/** Returns true when a metrics timestamp represents a real sample rather than a zero Go time. */
function hasMetricTimestamp(value?: string): value is string {
  return Boolean(value && !value.startsWith('0001-01-01'))
}

/** Renders a reusable numeric health card. */
function MetricCard({ label, value, detail, tone, icon }: { label: string; value: string; detail: string; tone: 'good' | 'warning' | 'neutral'; icon: ReactNode }) {
  return (
    <article className={`metric-card metric-${tone}`}>
      <div className="metric-top"><span className="metric-icon">{icon}</span><span className="metric-kicker">{label}</span><ArrowUpRight size={14} className="metric-arrow" /></div>
      <div className="metric-value">{value}</div>
      <div className="metric-detail">{detail}</div>
    </article>
  )
}

/** Renders a section heading with a compact state marker. */
function PanelHeading({ eyebrow, title, action, tone }: { eyebrow: string; title: string; action: string; tone: 'good' | 'warning' | 'neutral' }) {
  return (
    <div className="panel-heading">
      <div><span className="panel-eyebrow">{eyebrow}</span><h2>{title}</h2></div>
      <span className={`panel-action panel-action-${tone}`}><span />{action}</span>
    </div>
  )
}

/** Renders recent warning Events in a readable dashboard list. */
function WarningList({ warnings }: { warnings: ClusterOverview['warnings'] }) {
  return (
    <div className="warning-list">
      {warnings.map((warning, index) => (
        <div className="warning-row" key={`${warning.reason}-${warning.object}-${index}`}>
          <span className="warning-index">{String(index + 1).padStart(2, '0')}</span>
          <div className="warning-main"><strong>{warning.reason || 'Warning Event'}</strong><span>{warning.message}</span></div>
          <div className="warning-meta"><code>{warning.object || 'cluster'}</code><span>{relativeTime(warning.lastSeen)}</span></div>
        </div>
      ))}
    </div>
  )
}

/** Renders a single posture statistic row. */
function PostureRow({ icon, label, value, note, tone = 'neutral' }: { icon: ReactNode; label: string; value: string; note: string; tone?: 'good' | 'warning' | 'neutral' }) {
  return <div className="posture-row"><span className={`posture-icon posture-${tone}`}>{icon}</span><div><strong>{label}</strong><span>{note}</span></div><b>{value}</b></div>
}

/** Renders a cluster setup state when no kubeconfig contexts are available. */
function SetupState({ error }: { error?: string }) {
  return (
    <div className="state-page">
      <div className="state-card setup-card">
        <img src="/haggis.svg" alt="KubeHaggis" className="state-mark" />
        <span className="eyebrow"><span className="eyebrow-mark" /> Local signal missing</span>
        <h1>KubeHaggis needs a kubeconfig.</h1>
        <p>Point the local server at your standard kubeconfig, then reload this page. KubeHaggis never asks the browser to carry cluster credentials.</p>
        <div className="setup-command"><span>$</span><code>echo $KUBECONFIG || echo ~/.kube/config</code><button className="copy-button" aria-label="Copy kubeconfig command"><Command size={15} /></button></div>
        {error && <div className="inline-error"><AlertTriangle size={15} />{error}</div>}
      </div>
    </div>
  )
}

/** Renders a cluster connection error with the selected context identity. */
function ConnectionState({ context, error }: { context?: ContextSummary; error: string }) {
  return <div className="state-page"><div className="state-card"><span className="state-icon state-icon-warning"><AlertTriangle size={22} /></span><span className="eyebrow"><span className="eyebrow-mark" /> Connection issue</span><h1>Could not read this cluster.</h1><p>{context?.name || 'Selected context'} responded with an error before the overview could load.</p><div className="error-block"><code>{error}</code></div><button className="secondary-button" onClick={() => window.location.reload()}><RefreshCw size={15} /> Try again</button></div></div>
}

/** Renders a neutral loading state while Kubernetes data is being read. */
function LoadingState({ label }: { label: string }) {
  return <div className="state-page"><div className="loading-state"><LoaderCircle size={24} className="spin" /><span>{label}</span></div></div>
}

/** Renders an empty state with a concise next action. */
function EmptyState({ title, detail }: { title: string; detail: string }) {
  return <div className="state-page"><div className="state-card"><span className="state-icon"><Box size={22} /></span><span className="eyebrow"><span className="eyebrow-mark" /> Quiet field</span><h1>{title}</h1><p>{detail}</p></div></div>
}

/** Renders the generic live-updating resource table. */
function ResourceListPage({ context, namespace, definition, writeEnabled, onOpen, onCreate }: { context: string; namespace: string; definition: ResourceDefinition; writeEnabled: boolean; onOpen: (name: string, namespace?: string) => void; onCreate: () => void }) {
  const [response, setResponse] = useState<ResourceListResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [labelSelector, setLabelSelector] = useState('')
  const [watching, setWatching] = useState(false)

  useEffect(() => {
    let active = true
    let stream: EventSource | undefined
    const load = () => {
      setLoading(true)
      fetchResources(context, definition, namespace, labelSelector)
        .then((data) => {
          if (!active) return
          setResponse(data)
          setError('')
          if (!stream && data.resourceVersion) {
            stream = new EventSource(watchUrl(context, definition, namespace, data.resourceVersion))
            setWatching(true)
            stream.onmessage = () => load()
            stream.onerror = () => setWatching(false)
          }
        })
        .catch((loadError: Error) => active && setError(loadError.message))
        .finally(() => active && setLoading(false))
    }
    load()
    return () => {
      active = false
      stream?.close()
      setWatching(false)
    }
  }, [context, definition, namespace, labelSelector])

  if (!context) return <SetupState />
  const filtered = response?.items.filter((item) => item.name.toLowerCase().includes(search.toLowerCase())) ?? []

  return (
    <div className="resource-page">
      <section className="resource-intro">
        <div><div className="eyebrow"><span className="eyebrow-mark" /> Resource collection</div><h1>{titleCase(definition.resource)}</h1><p>{definition.namespaced ? 'Namespaced resource' : 'Cluster-scoped resource'} / {namespace === 'all' || !definition.namespaced ? 'all namespaces' : namespace}</p></div>
        <div className="resource-intro-meta"><span className={watching ? 'live-chip' : 'quiet-chip'}><span />{watching ? 'Live watch' : 'Refresh fallback'}</span><span>{response?.items.length ?? 0} objects</span></div>
      </section>
      <div className="table-toolbar">
        <label className="search-field"><Search size={16} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder={`Search ${definition.resource}`} /><kbd>/</kbd></label>
        <label className="filter-field"><ListFilter size={15} /><input value={labelSelector} onChange={(event) => setLabelSelector(event.target.value)} placeholder="label selector" /></label>
        <button className="icon-button toolbar-action" onClick={() => window.location.reload()} aria-label="Refresh resources"><RefreshCw size={16} /></button>
        <button className="primary-button" disabled={!writeEnabled} onClick={onCreate} title={writeEnabled ? `Create a ${definition.kind}` : 'Enable write mode to create resources'}><span>+</span> New {definition.kind}</button>
      </div>
      {error ? <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div> : null}
      <div className="resource-table-wrap">
        <table className="resource-table"><thead><tr><th><span className="table-check" /></th><th>Name</th><th>Namespace</th><th>Status</th><th>Ready</th><th>Age</th><th>Labels</th><th /></tr></thead><tbody>
          {loading && !response ? <tr><td colSpan={8}><div className="table-loading"><LoaderCircle size={18} className="spin" /> Reading API objects</div></td></tr> : null}
          {!loading && !filtered.length ? <tr><td colSpan={8}><div className="table-empty"><Box size={20} /><strong>No matching objects</strong><span>Try a different search or label selector.</span></div></td></tr> : null}
          {filtered.map((item) => <ResourceRow key={`${item.namespace}/${item.name}`} item={item} onOpen={onOpen} />)}
        </tbody></table>
      </div>
      <div className="table-footer"><span>Showing {filtered.length} of {response?.items.length ?? 0}</span><span><span className="table-footer-dot" /> updates arrive through the Kubernetes watch</span></div>
    </div>
  )
}

/** Renders one row in the generic resource table. */
function ResourceRow({ item, onOpen }: { item: ResourceSummary; onOpen: (name: string, namespace?: string) => void }) {
  const statusTone = item.status.toLowerCase() === 'true' || item.status.toLowerCase() === 'running' || item.status.toLowerCase() === 'active' ? 'good' : item.status.toLowerCase() === 'failed' || item.status.toLowerCase() === 'false' ? 'warning' : 'neutral'
  const labels = Object.entries(item.labels ?? {}).slice(0, 2)
  return <tr onClick={() => onOpen(item.name, item.namespace)}><td><span className="table-check" /></td><td><button className="resource-name" onClick={(event) => { event.stopPropagation(); onOpen(item.name, item.namespace) }}>{item.name}</button><span className="resource-uid">{item.uid.slice(0, 12)}</span></td><td><span className="namespace-cell">{item.namespace || 'cluster'}</span></td><td><span className={`status-pill status-${statusTone}`}><span />{item.status}</span></td><td><code className="ready-cell">{item.ready || '—'}</code></td><td><span className="age-cell">{relativeTime(item.createdAt)}</span></td><td><div className="label-stack">{labels.map(([key, value]) => <span key={key}>{key}={value}</span>)}{Object.keys(item.labels ?? {}).length > 2 && <span>+{Object.keys(item.labels ?? {}).length - 2}</span>}</div></td><td><button className="icon-button row-menu" aria-label={`Actions for ${item.name}`} onClick={(event) => event.stopPropagation()}><MoreHorizontal size={16} /></button></td></tr>
}

/** Renders the tabbed detail view for one generic resource. */
function ResourceDetailPage({ context, namespace, definition, name, definitions, writeEnabled, onBack, onOpenEvent, onOpenResource }: { context: string; namespace: string; definition: ResourceDefinition; name: string; definitions: ResourceDefinition[]; writeEnabled: boolean; onBack: () => void; onOpenEvent: (event: EventSummary) => void; onOpenResource: (definition: ResourceDefinition, name: string, namespace?: string) => void }) {
  const [object, setObject] = useState<ResourceObject | null>(null)
  const [yamlText, setYamlText] = useState('')
  const [originalYaml, setOriginalYaml] = useState('')
  const [secretsRevealed, setSecretsRevealed] = useState(false)
  const [tab, setTab] = useState('overview')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [reviewReady, setReviewReady] = useState(false)
  const isSecret = definition.resource === 'secrets'
  const isNew = name === 'new'

  useEffect(() => {
    let active = true
    setTab('overview')
    if (isNew) {
      const initial: ResourceObject = {
        apiVersion: definition.group ? `${definition.group}/${definition.version}` : definition.version,
        kind: definition.kind,
        metadata: definition.namespaced ? { namespace: namespace === 'all' ? '' : namespace } : {},
      }
      const text = stringify(initial)
      setObject(initial)
      setYamlText(text)
      setOriginalYaml('')
      return () => {
        active = false
      }
    }
    fetchResource(context, definition, namespace, name)
      .then((data) => {
        if (!active) return
        const editorObject = sanitizeEditorObject(data)
        const text = stringify(isSecret ? redactSecret(editorObject) : editorObject)
        setObject(data)
        setYamlText(text)
        setOriginalYaml(text)
        setError('')
      })
      .catch((loadError: Error) => active && setError(loadError.message))
    return () => {
      active = false
    }
  }, [context, definition, namespace, name, isNew, isSecret])

  /** Runs Kubernetes dry-run validation for the edited resource document. */
  async function reviewChanges() {
    setSaving(true)
    try {
      await applyResource(context, definition, namespace, yamlText, true)
      setReviewReady(true)
      setError('')
    } catch (reviewError) {
      setError((reviewError as Error).message)
    } finally {
      setSaving(false)
    }
  }

  /** Applies a previously reviewed resource document to Kubernetes. */
  async function saveChanges() {
    setSaving(true)
    try {
      const applied = await applyResource(context, definition, namespace, yamlText, false)
      const appliedName = isNew ? metadataString(applied, 'name') : name
      const appliedNamespace = definition.namespaced && namespace === 'all' ? metadataString(applied, 'namespace') : namespace
      let refreshed = applied
      let refreshError = ''
      if (appliedName) {
        try {
          refreshed = await fetchResource(context, definition, appliedNamespace, appliedName)
        } catch (caughtError) {
          refreshError = `Resource applied, but refresh failed: ${(caughtError as Error).message}`
        }
      }
      const text = stringify(sanitizeEditorObject(refreshed))
      setObject(refreshed)
      setYamlText(text)
      setOriginalYaml(text)
      setReviewReady(false)
      setError(refreshError)
      setTab('overview')
    } catch (saveError) {
      setError((saveError as Error).message)
    } finally {
      setSaving(false)
    }
  }

  /** Refreshes the complete Pod after an operational subresource changes it. */
  async function refreshObject() {
    const refreshed = await fetchResource(context, definition, namespace, name)
    const editorObject = sanitizeEditorObject(refreshed)
    const text = stringify(isSecret ? redactSecret(editorObject) : editorObject)
    setObject(refreshed)
    setYamlText(text)
    setOriginalYaml(text)
    setError('')
  }

  if (error && !object) return <ConnectionState error={error} />
  if (!object) return <LoadingState label={`Opening ${name}`} />
  const dirty = yamlText !== originalYaml
  const detailTabs = definition.resource === 'events' ? ['overview', 'yaml', 'related'] : definition.resource === 'pods' && !isNew ? ['overview', 'debug', 'files', 'yaml', 'events', 'terminal', 'port-forward', 'logs', 'related'] : definition.resource === 'pods' ? ['overview', 'yaml', 'events', 'terminal', 'port-forward', 'logs', 'related'] : ['overview', 'yaml', 'events', 'related']
  const resourceNamespace = definition.namespaced ? metadataString(object, 'namespace') || (namespace === 'all' ? '' : namespace) : ''

  return <div className="detail-page">
    <div className="detail-breadcrumb"><button className="back-link" onClick={onBack}>← {titleCase(definition.resource)}</button><span>/</span><strong>{isNew ? 'new' : name}</strong></div>
    <section className="detail-heading"><div><div className="eyebrow"><span className="eyebrow-mark" /> {isNew ? 'Create resource' : 'Resource detail'}</div><h1>{isNew ? `New ${definition.kind}` : name}</h1><p>{definition.kind} / {namespace === 'all' || !definition.namespaced ? 'cluster-scoped' : namespace}</p></div><div className="detail-actions"><span className="status-pill status-good"><span /> {isNew ? 'draft' : 'loaded'}</span><button className="secondary-button" disabled={!writeEnabled || !dirty || saving} onClick={reviewChanges}><ShieldCheck size={15} /> Review changes</button></div></section>
    <div className="detail-tabs" role="tablist">{detailTabs.map((item) => <button key={item} className={tab === item ? 'detail-tab active' : 'detail-tab'} onClick={() => setTab(item)}>{item === 'yaml' ? 'YAML editor' : titleCase(item)}</button>)}</div>
    {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
    {tab === 'overview' && <div className="detail-grid"><div className="panel detail-panel"><PanelHeading eyebrow="Identity" title="Resource coordinates" action="Stable object" tone="good" /><div className="identity-list"><IdentityRow label="Kind" value={definition.kind} /><IdentityRow label="Namespace" value={namespace === 'all' || !definition.namespaced ? 'cluster-scoped' : namespace} /><IdentityRow label="UID" value={String(object.metadata && (object.metadata as Record<string, unknown>).uid || '—')} mono /><IdentityRow label="Resource version" value={String(object.metadata && (object.metadata as Record<string, unknown>).resourceVersion || '—')} mono /></div></div><div className="panel detail-panel"><PanelHeading eyebrow="Fields" title="Top-level shape" action={`${Object.keys(object).length} sections`} tone="neutral" /><div className="field-cloud">{Object.keys(object).map((key) => <span key={key}><span />{key}</span>)}</div></div></div>}
    {tab === 'yaml' && <div className="editor-panel"><div className="editor-toolbar"><div><span className="editor-dot" /><strong>{isSecret ? 'Secret YAML' : 'Resource YAML'}</strong><span>{dirty ? 'unsaved changes' : 'synced with cluster'}</span></div><div>{!writeEnabled && <span className="editor-lock"><LockKeyhole size={13} /> Enable write mode to edit</span>}{reviewReady && <span className="review-ready"><Check size={13} /> Dry-run passed</span>}</div></div>{isSecret && <div className="secret-notice"><LockKeyhole size={15} /><span>{secretsRevealed ? 'Secret values are visible in this editor.' : 'Secret values are masked by default.'}</span>{secretsRevealed ? <button className="reveal-button" onClick={() => { setSecretsRevealed(false); const safeText = stringify(redactSecret(object)); setYamlText(safeText); setOriginalYaml(safeText) }}>Mask values</button> : <button className="reveal-button" onClick={() => { setSecretsRevealed(true); const rawText = stringify(sanitizeEditorObject(object)); setYamlText(rawText); setOriginalYaml(rawText) }}>Reveal values</button>}</div>}<textarea className="yaml-editor" value={yamlText} onChange={(event) => { setYamlText(event.target.value); setReviewReady(false) }} readOnly={!writeEnabled || (isSecret && !secretsRevealed)} spellCheck={false} aria-label="Resource YAML" /><div className="editor-footer"><span><FileCode2 size={14} /> client-side apply / full object update</span>{reviewReady && writeEnabled && <button className="primary-button" onClick={saveChanges} disabled={saving}>{saving ? <LoaderCircle size={15} className="spin" /> : <Check size={15} />} Apply reviewed changes</button>}</div></div>}
      {tab === 'logs' && definition.resource === 'pods' && <PodLogsPanel context={context} namespace={namespace} pod={name} />}
      {tab === 'terminal' && definition.resource === 'pods' && <PodTerminalPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} />}
      {tab === 'port-forward' && definition.resource === 'pods' && <PodPortForwardPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} />}
      {tab === 'debug' && definition.resource === 'pods' && !isNew && <PodDebugPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} writeEnabled={writeEnabled} onRefresh={refreshObject} />}
      {tab === 'files' && definition.resource === 'pods' && !isNew && <PodFileCopyPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} writeEnabled={writeEnabled} />}
      {tab === 'events' && <ResourceEventsPanel context={context} namespace={namespace} uid={metadataString(object, 'uid')} onOpenEvent={onOpenEvent} />}
    {tab === 'related' && <OwnerReferencesPanel object={object} definitions={definitions} namespace={resourceNamespace} onOpenResource={onOpenResource} />}
  </div>
}

/** Renders standard Kubernetes owner relationships for a resource. */
function OwnerReferencesPanel({ object, definitions, namespace, onOpenResource }: { object: ResourceObject; definitions: ResourceDefinition[]; namespace: string; onOpenResource: (definition: ResourceDefinition, name: string, namespace?: string) => void }) {
  const owners = readOwnerReferences(object)

  if (!owners.length) {
    return <QuietState icon={<Network size={22} />} title="No owner references" detail="Kubernetes did not declare an owner for this object." />
  }

  return <div className="relationship-panel">
    <div className="panel relationship-summary-panel">
      <PanelHeading eyebrow="Relationships" title="Owner references" action={`${owners.length} ${owners.length === 1 ? 'reference' : 'references'}`} tone="neutral" />
      <p className="relationship-copy">These links come from Kubernetes <code>metadata.ownerReferences</code>. Controller owners are marked when Kubernetes identifies one.</p>
    </div>
    <div className="relationship-list">
      {owners.map((owner, index) => {
        const ownerDefinition = resolveOwnerDefinition(owner, definitions)
        const ownerNamespace = ownerDefinition?.namespaced ? namespace : undefined
        const row = <>
          <span className="relationship-icon"><Network size={16} /></span>
          <div className="relationship-main">
            <div className="relationship-labels"><span className="relationship-kind">{owner.controller ? 'Controller owner' : 'Owner reference'}</span>{owner.blockOwnerDeletion && <span className="relationship-flag">Deletion blocked</span>}</div>
            <strong>{owner.kind}/{owner.name}</strong>
            <span className="relationship-meta"><code>{owner.apiVersion}</code>{owner.uid ? <><span>·</span><code>uid {owner.uid.slice(0, 12)}</code></> : null}</span>
          </div>
          {!ownerDefinition && <span className="relationship-unresolved">Not discovered</span>}
        </>
        return ownerDefinition
          ? <button type="button" className="relationship-row relationship-row-button" key={`${owner.apiVersion}/${owner.kind}/${owner.name}/${owner.uid || index}`} aria-label={`Open owner ${owner.kind}/${owner.name}`} onClick={() => onOpenResource(ownerDefinition, owner.name, ownerNamespace)}>{row}</button>
          : <div className="relationship-row" key={`${owner.apiVersion}/${owner.kind}/${owner.name}/${owner.uid || index}`}>{row}</div>
      })}
    </div>
  </div>
}

/** Renders the focused ephemeral debug-container launcher for a Pod. */
function PodDebugPanel({ context, namespace, pod, object, writeEnabled, onRefresh }: { context: string; namespace: string; pod: string; object: ResourceObject; writeEnabled: boolean; onRefresh: () => Promise<void> }) {
  const [name, setName] = useState('debugger')
  const [image, setImage] = useState('busybox:1.36')
  const [command, setCommand] = useState('/bin/sh')
  const [target, setTarget] = useState('')
  const [launching, setLaunching] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const targets = podTargetContainerNames(object)

  useEffect(() => {
    if (target && !targets.includes(target)) setTarget('')
  }, [object])

  /** Sends the configured debug-container fields to Kubernetes. */
  async function launch() {
    if (!writeEnabled) {
      setError('Enable write mode before launching an ephemeral debug container.')
      return
    }
    const commandArgs = command.split('\n').map((argument) => argument.trim()).filter(Boolean)
    if (!name.trim()) {
      setError('A debug container name is required.')
      return
    }
    if (!image.trim()) {
      setError('A debug container image is required.')
      return
    }
    setLaunching(true)
    setError('')
    setSuccess('')
    try {
      const request: EphemeralContainerRequest = { name: name.trim(), image: image.trim(), command: commandArgs, targetContainerName: target }
      await launchEphemeralContainer(context, namespace, pod, request)
      setSuccess(`Ephemeral container ${request.name} was sent to Kubernetes.`)
      try {
        await onRefresh()
      } catch (refreshError) {
        setError(`Debug container launched, but the Pod refresh failed: ${(refreshError as Error).message}`)
      }
    } catch (launchError) {
      setError((launchError as Error).message)
    } finally {
      setLaunching(false)
    }
  }

  return <div className="pod-operation-panel">
    <div className="pod-operation-header"><div className="logs-title"><Bug size={16} /><strong>Ephemeral debug container</strong><span>pods/ephemeralcontainers</span></div><span className="pod-operation-scope">write operation</span></div>
    <div className="pod-operation-intro"><strong>Attach a temporary troubleshooting container.</strong><span>Kubernetes adds this container to the existing Pod. It is not part of the workload template and cannot be restarted or removed through this workflow.</span></div>
    {!writeEnabled && <div className="operation-lock"><LockKeyhole size={14} /> Enable write mode to launch a debug container. Kubernetes RBAC still authorizes the subresource.</div>}
    <form className="pod-operation-form" onSubmit={(event) => { event.preventDefault(); void launch() }}>
      <div className="pod-operation-grid">
        <label className="pod-operation-field"><span>Name</span><input value={name} onChange={(event) => setName(event.target.value)} disabled={!writeEnabled || launching} placeholder="debugger" /></label>
        <label className="pod-operation-field"><span>Image</span><input value={image} onChange={(event) => setImage(event.target.value)} disabled={!writeEnabled || launching} placeholder="busybox:1.36" /></label>
        <label className="pod-operation-field"><span>Target container <em>optional</em></span><select value={target} onChange={(event) => setTarget(event.target.value)} disabled={!writeEnabled || launching}><option value="">Pod namespaces only</option>{targets.map((container) => <option key={container} value={container}>{container}</option>)}</select></label>
        <label className="pod-operation-field pod-operation-field-wide"><span>Command <em>one argument per line</em></span><textarea value={command} onChange={(event) => setCommand(event.target.value)} disabled={!writeEnabled || launching} rows={3} placeholder="/bin/sh" spellCheck={false} /></label>
      </div>
      <div className="pod-operation-actions"><span>Image pull and runtime support are controlled by the cluster.</span><button type="submit" className="primary-button" disabled={!writeEnabled || launching}>{launching ? <LoaderCircle size={15} className="spin" /> : <Bug size={15} />} {launching ? 'Launching' : 'Launch debug container'}</button></div>
    </form>
    {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
    {success && <div className="operation-success"><Check size={15} />{success}</div>}
    <div className="pod-operation-footer"><span><Bug size={14} /> Native typed Kubernetes update</span><span>Unsupported subresources and RBAC denials are reported by the API.</span></div>
  </div>
}

/** Renders read-only Pod downloads and write-mode container uploads. */
function PodFileCopyPanel({ context, namespace, pod, object, writeEnabled }: { context: string; namespace: string; pod: string; object: ResourceObject; writeEnabled: boolean }) {
  const [container, setContainer] = useState('')
  const [downloadPath, setDownloadPath] = useState('/tmp/example.txt')
  const [uploadPath, setUploadPath] = useState('/tmp/example.txt')
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState<'download' | 'upload' | ''>('')
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const containers = podContainerNames(object)

  useEffect(() => {
    if (container && !containers.includes(container)) setContainer('')
  }, [object])

  /** Downloads a regular container file through the native exec bridge. */
  async function download() {
    if (!downloadPath.trim()) {
      setError('A remote path is required for download.')
      return
    }
    setBusy('download')
    setError('')
    setSuccess('')
    try {
      const blob = await downloadPodFile(context, namespace, pod, container, downloadPath.trim())
      const objectUrl = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = objectUrl
      anchor.download = remoteFileName(downloadPath)
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      window.setTimeout(() => URL.revokeObjectURL(objectUrl), 0)
      setSuccess(`Downloaded ${remoteFileName(downloadPath)} from the container.`)
    } catch (downloadError) {
      setError((downloadError as Error).message)
    } finally {
      setBusy('')
    }
  }

  /** Uploads a selected browser file to the exact remote path in write mode. */
  async function upload() {
    if (!writeEnabled) {
      setError('Enable write mode before uploading a file into a container.')
      return
    }
    if (!file) {
      setError('Choose a local file before uploading.')
      return
    }
    if (!uploadPath.trim()) {
      setError('A remote destination path is required for upload.')
      return
    }
    setBusy('upload')
    setError('')
    setSuccess('')
    try {
      await uploadPodFile(context, namespace, pod, container, uploadPath.trim(), file)
      setSuccess(`Uploaded ${file.name} to the container.`)
    } catch (uploadError) {
      setError((uploadError as Error).message)
    } finally {
      setBusy('')
    }
  }

  /** Updates the local file and suggests a safe temporary destination. */
  function selectFile(selected: File | undefined) {
    setFile(selected || null)
    if (selected && (!uploadPath.trim() || uploadPath === '/tmp/example.txt')) setUploadPath(`/tmp/${selected.name}`)
  }

  return <div className="pod-operation-panel">
    <div className="pod-operation-header"><div className="logs-title"><Download size={16} /><strong>Container file transfer</strong><span>native pods/exec stream</span></div><span className="pod-operation-scope">download read-only / upload write</span></div>
    <div className="pod-operation-intro"><strong>Move one regular file at a time.</strong><span>Downloads read container data and are available in read-only mode. Uploads change the container filesystem and require write mode. Both actions still require Kubernetes <code>pods/exec</code> permission.</span></div>
    <div className="pod-operation-shared"><label className="pod-operation-field"><span>Container</span><select value={container} onChange={(event) => setContainer(event.target.value)} disabled={Boolean(busy)}><option value="">Default container</option>{containers.map((name) => <option key={name} value={name}>{name}</option>)}</select></label></div>
    <div className="copy-operation-grid">
      <section className="copy-operation-card"><div className="copy-operation-heading"><span className="copy-operation-icon"><Download size={16} /></span><div><strong>Download from container</strong><span>Read-only native exec</span></div></div><label className="pod-operation-field"><span>Absolute remote path</span><input value={downloadPath} onChange={(event) => setDownloadPath(event.target.value)} disabled={Boolean(busy)} placeholder="/var/log/app.log" /></label><button type="button" className="secondary-button" onClick={() => void download()} disabled={Boolean(busy)}>{busy === 'download' ? <LoaderCircle size={15} className="spin" /> : <Download size={15} />} {busy === 'download' ? 'Downloading' : 'Download file'}</button></section>
      <section className="copy-operation-card"><div className="copy-operation-heading"><span className="copy-operation-icon copy-operation-icon-upload"><Upload size={16} /></span><div><strong>Upload into container</strong><span>{writeEnabled ? 'Write mode enabled' : 'Enable write mode first'}</span></div></div><label className="pod-operation-field"><span>Local file</span><input type="file" onChange={(event) => selectFile(event.target.files?.[0])} disabled={!writeEnabled || Boolean(busy)} /></label><label className="pod-operation-field"><span>Absolute remote destination</span><input value={uploadPath} onChange={(event) => setUploadPath(event.target.value)} disabled={!writeEnabled || Boolean(busy)} placeholder="/tmp/config.txt" /></label><button type="button" className="primary-button" onClick={() => void upload()} disabled={!writeEnabled || Boolean(busy)}>{busy === 'upload' ? <LoaderCircle size={15} className="spin" /> : <Upload size={15} />} {busy === 'upload' ? 'Uploading' : 'Upload file'}</button></section>
    </div>
    {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
    {success && <div className="operation-success"><Check size={15} />{success}</div>}
    <div className="pod-operation-footer"><span><Download size={14} /> Files are streamed through the local server</span><span>Container images must provide tar for copy operations.</span></div>
  </div>
}

/** Renders a streaming native Pod log panel with operator controls. */
function PodLogsPanel({ context, namespace, pod }: { context: string; namespace: string; pod: string }) {
  const [logs, setLogs] = useState('')
  const [container, setContainer] = useState('')
  const [follow, setFollow] = useState(true)
  const [previous, setPrevious] = useState(false)
  const [tailLines, setTailLines] = useState(200)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    /** Loads and incrementally appends the native Pod log response. */
    async function loadLogs() {
      setLoading(true)
      setError('')
      try {
        const response = await fetch(logsUrl(context, namespace, pod, container, follow, previous, tailLines), { signal: controller.signal })
        if (!response.ok) {
          const payload = (await response.json().catch(() => ({}))) as { error?: string }
          throw new Error(payload.error || `Log request failed with ${response.status}`)
        }
        if (!response.body) {
          setLogs(await response.text())
          return
        }
        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        let output = ''
        while (true) {
          const chunk = await reader.read()
          if (chunk.done) break
          output += decoder.decode(chunk.value, { stream: true })
          setLogs(output)
        }
        output += decoder.decode()
        setLogs(output)
      } catch (loadError) {
        if ((loadError as Error).name !== 'AbortError') setError((loadError as Error).message)
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }
    loadLogs()
    return () => controller.abort()
  }, [context, namespace, pod, container, follow, previous, tailLines])

  return <div className="logs-panel"><div className="logs-toolbar"><div className="logs-title"><TerminalSquare size={16} /><strong>Container output</strong><span>{follow ? 'following stream' : 'snapshot'}</span></div><div className="logs-controls"><label><span>Container</span><input value={container} onChange={(event) => setContainer(event.target.value)} placeholder="default" /></label><label><span>Tail</span><select value={tailLines} onChange={(event) => setTailLines(Number(event.target.value))}><option value={100}>100 lines</option><option value={200}>200 lines</option><option value={500}>500 lines</option><option value={1000}>1,000 lines</option></select></label><label className="check-control"><input type="checkbox" checked={previous} onChange={(event) => setPrevious(event.target.checked)} /><span>Previous</span></label><label className="check-control"><input type="checkbox" checked={follow} onChange={(event) => setFollow(event.target.checked)} /><span>Follow</span></label></div></div>{error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}<pre className="log-output">{loading && !logs ? <span className="log-placeholder"><LoaderCircle size={17} className="spin" /> Reading container output</span> : logs || 'No log lines returned.'}</pre></div>
}

/** Renders an interactive native exec or attach terminal for a Pod. */
function PodTerminalPanel({ context, namespace, pod, object }: { context: string; namespace: string; pod: string; object: ResourceObject }) {
  const [mode, setMode] = useState<'exec' | 'attach'>('exec')
  const [container, setContainer] = useState('')
  const [command, setCommand] = useState('/bin/sh')
  const [status, setStatus] = useState<'idle' | 'connecting' | 'connected' | 'closed'>('idle')
  const [error, setError] = useState('')
  const socketRef = useRef<WebSocket | null>(null)
  const terminalHostRef = useRef<HTMLDivElement | null>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const containers = podContainerNames(object)

  useEffect(() => {
    if (!container && containers.length) setContainer(containers[0])
  }, [object])

  useEffect(() => {
    const host = terminalHostRef.current
    if (!host) return
    const terminal = new Terminal({
      convertEol: true,
      cursorBlink: true,
      disableStdin: true,
      fontFamily: "'DM Mono', 'SFMono-Regular', Consolas, monospace",
      fontSize: 12,
      lineHeight: 1.35,
      scrollback: 5000,
      theme: {
        background: '#0b0e0b',
        foreground: '#b7c8aa',
        cursor: '#f09a62',
        selectionBackground: 'rgba(215, 123, 77, .35)',
      },
    })
    const fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(host)
    fitAddon.fit()
    terminalRef.current = terminal
    fitAddonRef.current = fitAddon

    const dataSubscription = terminal.onData((data) => {
      const socket = socketRef.current
      if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'input', data }))
    })
    const resizeSubscription = terminal.onResize(({ cols, rows }) => sendTerminalResize(socketRef.current, cols, rows))
    const observer = new ResizeObserver(() => fitAddon.fit())
    observer.observe(host)
    return () => {
      observer.disconnect()
      dataSubscription.dispose()
      resizeSubscription.dispose()
      terminal.dispose()
      terminalRef.current = null
      fitAddonRef.current = null
    }
  }, [])

  useEffect(() => {
    const terminal = terminalRef.current
    if (!terminal) return
    const connected = status === 'connected'
    terminal.options.disableStdin = !connected
    terminal.options.cursorBlink = connected
    if (!connected) terminal.blur()
  }, [status])

  useEffect(() => {
    return () => {
      socketRef.current?.close()
      socketRef.current = null
      terminalRef.current?.reset()
    }
  }, [context, namespace, pod])

  /** Opens a browser WebSocket backed by a Kubernetes TTY session. */
  function connect() {
    if (!namespace || namespace === 'all') {
      setError('A Pod namespace is required to open a terminal session.')
      return
    }
    socketRef.current?.close()
    terminalRef.current?.reset()
    setError('')
    setStatus('connecting')
    const socket = new WebSocket(terminalUrl(context, namespace, pod, container, mode, command))
    socketRef.current = socket
    socket.onopen = () => {
      if (socketRef.current !== socket) return
      setStatus('connected')
      fitAddonRef.current?.fit()
      terminalRef.current?.focus()
    }
    socket.onmessage = (event) => {
      let message: TerminalMessage
      try {
        message = JSON.parse(event.data) as TerminalMessage
      } catch {
        setError('The terminal returned an invalid message.')
        return
      }
      if (message.type === 'output') terminalRef.current?.write(message.data || '')
      if (message.type === 'error') setError(message.message || 'The terminal session failed.')
      if (message.type === 'closed') setStatus('closed')
    }
    socket.onerror = () => setError((current) => current || 'The terminal connection failed.')
    socket.onclose = () => {
      if (socketRef.current === socket) socketRef.current = null
      setStatus('closed')
    }
  }

  /** Closes the current browser and Kubernetes terminal session. */
  function disconnect() {
    const socket = socketRef.current
    if (!socket) return
    if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'close' }))
    socket.close()
    socketRef.current = null
    setStatus('closed')
  }

  const active = status === 'connecting' || status === 'connected'
  const statusLabel = status === 'connected' ? 'Connected' : status === 'connecting' ? 'Connecting' : status === 'closed' ? 'Disconnected' : 'Not connected'
  const disconnected = status !== 'connected'
  return <div className="terminal-panel">
    <div className="terminal-toolbar">
      <div className="logs-title"><TerminalSquare size={16} /><strong>Pod terminal</strong><span className={`terminal-session-status terminal-session-status-${status}`}><span />{statusLabel}</span></div>
      <div className="terminal-controls">
        <label><span>Mode</span><select value={mode} onChange={(event) => setMode(event.target.value as 'exec' | 'attach')} disabled={active}><option value="exec">Exec</option><option value="attach">Attach</option></select></label>
        <label><span>Container</span><select value={container} onChange={(event) => setContainer(event.target.value)} disabled={active}><option value="">Default</option>{containers.map((name) => <option key={name} value={name}>{name}</option>)}</select></label>
        {mode === 'exec' && <label><span>Command</span><input value={command} onChange={(event) => setCommand(event.target.value)} disabled={active} /></label>}
        {active ? <button className="secondary-button" onClick={disconnect}>Disconnect</button> : <button className="secondary-button" onClick={connect}>{status === 'closed' ? 'Reconnect' : 'Connect'}</button>}
      </div>
    </div>
    {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
    <div className={`terminal-surface ${disconnected ? 'terminal-surface-inactive' : ''}`}>
      <div ref={terminalHostRef} className="terminal-host" aria-label="Pod terminal" />
      {disconnected && <div className="terminal-overlay"><div><span className="terminal-overlay-icon"><TerminalSquare size={18} /></span><strong>{status === 'closed' ? 'Session disconnected' : 'Terminal not connected'}</strong><span>{status === 'closed' ? 'Reconnect to open a new TTY session.' : 'Connect to open a TTY session.'}</span></div></div>}
    </div>
    <div className="terminal-footer"><span><TerminalSquare size={14} /> TTY session / Kubernetes remote command</span><span>{mode === 'exec' ? 'command starts in a new process' : 'attached to the existing process'}</span></div>
  </div>
}

/** Describes a TCP port advertised by one Pod container. */
type PodPortOption = {
  container: string
  port: number
  name?: string
  protocol: string
}

/** Renders a native Pod port-forward session with local endpoint controls. */
function PodPortForwardPanel({ context, namespace, pod, object }: { context: string; namespace: string; pod: string; object: ResourceObject }) {
  const [container, setContainer] = useState('')
  const [targetPort, setTargetPort] = useState(0)
  const [localPort, setLocalPort] = useState(0)
  const [status, setStatus] = useState<'idle' | 'connecting' | 'connected' | 'closed'>('idle')
  const [error, setError] = useState('')
  const socketRef = useRef<WebSocket | null>(null)
  const containers = podContainerNames(object)
  const portOptions = podPortOptions(object)
  const selectedPorts = portOptions.filter((option) => !container || option.container === container)

  useEffect(() => {
    const firstContainer = containers[0] || ''
    const firstOption = portOptions.find((option) => option.container === firstContainer) || portOptions[0]
    setContainer(firstContainer)
    setTargetPort(firstOption?.port || 0)
    setLocalPort(firstOption?.port || 0)
    setError('')
    setStatus('idle')
  }, [object])

  useEffect(() => {
    return () => {
      socketRef.current?.close()
      socketRef.current = null
    }
  }, [context, namespace, pod])

  /** Opens a binary WebSocket backed by a Kubernetes Pod port-forward. */
  function connect() {
    if (!namespace || namespace === 'all') {
      setError('A Pod namespace is required to start port forwarding.')
      return
    }
    if (!Number.isInteger(targetPort) || targetPort < 1 || targetPort > 65535) {
      setError('Target port must be between 1 and 65535.')
      return
    }
    if (!Number.isInteger(localPort) || localPort < 1 || localPort > 65535) {
      setError('Local port must be between 1 and 65535.')
      return
    }

    socketRef.current?.close()
    setError('')
    setStatus('connecting')
    const socket = new WebSocket(portForwardUrl(context, namespace, pod, targetPort, localPort))
    socketRef.current = socket
    socket.onmessage = (event) => {
      if (socketRef.current !== socket) return
      if (typeof event.data !== 'string') return
      let message: PortForwardMessage
      try {
        message = JSON.parse(event.data) as PortForwardMessage
      } catch {
        setError('The port-forward returned an invalid control message.')
        return
      }
      if (message.type === 'ready') {
        setStatus('connected')
        return
      }
      if (message.type === 'error') {
        setError(message.message || 'The port-forward session failed.')
        setStatus('closed')
        socket.close()
        return
      }
      if (message.type === 'closed') setStatus('closed')
    }
    socket.onerror = () => {
      if (socketRef.current === socket) setError((current) => current || 'The port-forward connection failed.')
    }
    socket.onclose = () => {
      if (socketRef.current !== socket) return
      socketRef.current = null
      setStatus('closed')
    }
  }

  /** Closes the browser and Kubernetes port-forward session. */
  function disconnect() {
    socketRef.current?.close()
    socketRef.current = null
    setStatus('closed')
  }

  /** Selects the first known TCP port when the container changes. */
  function changeContainer(value: string) {
    setContainer(value)
    const firstPort = portOptions.find((option) => option.container === value)
    if (firstPort) {
      setTargetPort(firstPort.port)
      if (!localPort) setLocalPort(firstPort.port)
    }
  }

  const active = status === 'connecting' || status === 'connected'
  const statusLabel = status === 'connected' ? 'Forwarding' : status === 'connecting' ? 'Connecting' : status === 'closed' ? 'Disconnected' : 'Not started'
  return <div className="port-forward-panel">
    <div className="port-forward-header">
      <div className="logs-title"><Network size={16} /><strong>Pod port forward</strong><span className={`terminal-session-status terminal-session-status-${status}`}><span />{statusLabel}</span></div>
      <span className="port-forward-scope">read-only / Pod subresource</span>
    </div>
    <div className="port-forward-controls">
      <label className="port-forward-field"><span>Container</span><select value={container} onChange={(event) => changeContainer(event.target.value)} disabled={active}><option value="">Pod network</option>{containers.map((name) => <option key={name} value={name}>{name}</option>)}</select></label>
      <label className="port-forward-field"><span>Target port</span><input list="pod-port-forward-options" type="number" min="1" max="65535" value={targetPort || ''} onChange={(event) => setTargetPort(Number(event.target.value))} disabled={active} placeholder="8080" /><datalist id="pod-port-forward-options">{selectedPorts.map((option) => <option key={`${option.container}-${option.port}-${option.name || ''}`} value={option.port}>{`${option.container}${option.name ? ` / ${option.name}` : ''} (${option.protocol})`}</option>)}</datalist></label>
      <label className="port-forward-field"><span>Local port</span><input type="number" min="1" max="65535" value={localPort || ''} onChange={(event) => setLocalPort(Number(event.target.value))} disabled={active} placeholder="8080" /></label>
      {active ? <button className="secondary-button port-forward-action" onClick={disconnect}>Stop forward</button> : <button className="secondary-button port-forward-action" onClick={connect}>{status === 'closed' ? 'Restart forward' : 'Start forward'}</button>}
    </div>
    <div className="port-forward-note"><span>Known TCP container ports are suggested, but any numeric Pod target is valid.</span>{localPort > 0 && <code>127.0.0.1:{localPort}</code>}</div>
    {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
    <div className={`port-forward-status port-forward-status-${status}`}>
      <div><span className="port-forward-status-mark"><span /></span><div><strong>{status === 'connected' ? `Local :${localPort} is forwarding to Pod :${targetPort}` : status === 'connecting' ? 'Negotiating the native Kubernetes session' : status === 'closed' ? 'Forwarding session stopped' : 'No local session running'}</strong><span>{status === 'connected' ? 'Use the loopback endpoint from a local client.' : 'Kubernetes RBAC authorizes the port-forward request.'}</span></div></div>
    </div>
    <div className="port-forward-footer"><span><Network size={14} /> TCP / Kubernetes portforward subresource</span><span>{status === 'connected' ? 'Stop the session to release the local port.' : 'Start a session to bind the local endpoint.'}</span></div>
  </div>
}

/** Returns the container names available in a generic Pod object. */
function podContainerNames(object: ResourceObject): string[] {
  return podContainerNamesFromFields(object, ['containers', 'initContainers', 'ephemeralContainers'])
}

/** Returns regular Pod containers that Kubernetes can target with a debug container. */
function podTargetContainerNames(object: ResourceObject): string[] {
  return podContainerNamesFromFields(object, ['containers'])
}

/** Reads unique container names from selected PodSpec container arrays. */
function podContainerNamesFromFields(object: ResourceObject, fields: string[]): string[] {
  const spec = object.spec
  if (!spec || typeof spec !== 'object' || Array.isArray(spec)) return []
  const names: string[] = []
  for (const field of fields) {
    const entries = (spec as Record<string, unknown>)[field]
    if (!Array.isArray(entries)) continue
    for (const entry of entries) {
      if (!entry || typeof entry !== 'object' || Array.isArray(entry)) continue
      const name = (entry as Record<string, unknown>).name
      if (typeof name === 'string' && name && !names.includes(name)) names.push(name)
    }
  }
  return names
}

/** Returns a safe browser filename from an absolute remote file path. */
function remoteFileName(value: string): string {
  const name = value.trim().split('/').filter(Boolean).pop()
  return name || 'download'
}

/** Returns the TCP ports advertised by containers in a generic Pod object. */
function podPortOptions(object: ResourceObject): PodPortOption[] {
  const spec = object.spec
  if (!spec || typeof spec !== 'object' || Array.isArray(spec)) return []
  const options: PodPortOption[] = []
  for (const field of ['containers', 'initContainers', 'ephemeralContainers']) {
    const entries = (spec as Record<string, unknown>)[field]
    if (!Array.isArray(entries)) continue
    for (const entry of entries) {
      if (!entry || typeof entry !== 'object' || Array.isArray(entry)) continue
      const containerObject = entry as Record<string, unknown>
      const container = typeof containerObject.name === 'string' ? containerObject.name : ''
      const ports = containerObject.ports
      if (!container || !Array.isArray(ports)) continue
      for (const rawPort of ports) {
        if (!rawPort || typeof rawPort !== 'object' || Array.isArray(rawPort)) continue
        const portObject = rawPort as Record<string, unknown>
        const port = portObject.containerPort
        const protocol = typeof portObject.protocol === 'string' ? portObject.protocol.toUpperCase() : 'TCP'
        if (typeof port !== 'number' || !Number.isInteger(port) || port < 1 || port > 65535 || protocol !== 'TCP') continue
        const name = typeof portObject.name === 'string' && portObject.name ? portObject.name : undefined
        if (!options.some((option) => option.container === container && option.port === port && option.name === name)) options.push({ container, port, name, protocol })
      }
    }
  }
  return options
}

/** Sends the current terminal element dimensions to Kubernetes. */
function sendTerminalResize(socket: WebSocket | null, cols: number, rows: number) {
  if (!socket || socket.readyState !== WebSocket.OPEN) return
  socket.send(JSON.stringify({ type: 'resize', cols, rows }))
}

/** Renders the live Event stream associated with a resource UID. */
function ResourceEventsPanel({ context, namespace, uid, onOpenEvent }: { context: string; namespace: string; uid: string; onOpenEvent: (event: EventSummary) => void }) {
  const [events, setEvents] = useState<EventSummary[]>([])
  const [error, setError] = useState('')
  const [watching, setWatching] = useState(false)
  const [loading, setLoading] = useState(Boolean(uid))

  useEffect(() => {
    if (!uid) {
      setLoading(false)
      return
    }
    let active = true
    let stream: EventSource | undefined
    const load = () => {
      fetchResourceEvents(context, namespace === 'all' ? '' : namespace, uid)
        .then((response) => {
          if (!active) return
          setEvents(response.items)
          setError('')
          if (!stream && response.resourceVersion) {
            stream = new EventSource(resourceEventsWatchUrl(context, namespace === 'all' ? '' : namespace, uid, response.resourceVersion))
            setWatching(true)
            stream.onmessage = () => load()
            stream.onerror = () => setWatching(false)
          }
        })
        .catch((loadError: Error) => active && setError(loadError.message))
        .finally(() => active && setLoading(false))
    }
    load()
    return () => {
      active = false
      stream?.close()
      setWatching(false)
    }
  }, [context, namespace, uid])

  /** Opens a generic Event detail page only when Kubernetes supplied its name. */
  function openEvent(event: EventSummary) {
    if (!event.name) {
      setError('This Event has no Kubernetes name. Refresh the resource and try again.')
      return
    }
    onOpenEvent(event)
  }

  if (!uid) return <QuietState icon={<Activity size={22} />} title="Events need a resource UID" detail="This object does not expose a UID that can be used for native Event filtering." />

  return <div className="events-panel"><div className="events-toolbar"><div className="logs-title"><Activity size={16} /><strong>Resource Events</strong><span>{events.length} recorded</span></div><span className={watching ? 'live-chip' : 'quiet-chip'}><span />{watching ? 'Live watch' : 'Refresh fallback'}</span></div>{error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}{loading && !events.length ? <div className="events-loading"><LoaderCircle size={17} className="spin" /> Reading resource Events</div> : null}{!loading && !events.length && !error ? <QuietState icon={<ShieldCheck size={22} />} title="No Events for this resource" detail="Kubernetes has not reported an Event associated with this object." /> : null}<div className="event-list">{events.map((event, index) => <button type="button" className="event-row event-row-button" key={`${event.name}-${event.reason}-${event.lastSeen}-${index}`} onClick={() => openEvent(event)}><span className={`event-type event-type-${event.type.toLowerCase()}`}><span />{event.type || 'Normal'}</span><span className="event-copy"><strong>{event.reason || 'Event'}</strong><span>{event.message || 'No message provided.'}</span></span><span className="event-meta"><code>{event.count > 1 ? `x${event.count}` : 'once'}</code><span>{relativeTime(event.lastSeen)}</span></span></button>)}</div></div>
}

/** Renders a key/value identity row on a detail page. */
function IdentityRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="identity-row"><span>{label}</span><strong className={mono ? 'mono' : ''}>{value}</strong></div>
}

/** Renders a compact quiet state inside an overview panel. */
function QuietState({ icon, title, detail }: { icon: ReactNode; title: string; detail: string }) {
  return <div className="quiet-state"><span className="quiet-state-icon">{icon}</span><strong>{title}</strong><span>{detail}</span></div>
}

/** Renders the context selector with cluster identity details. */
function ContextPicker({ contexts, value, onChange }: { contexts: ContextSummary[]; value: string; onChange: (value: string) => void }) {
  return <label className="select-shell context-select"><span className="select-icon"><Server size={15} /></span><select value={value} onChange={(event) => onChange(event.target.value)} disabled={!contexts.length}><option value="">No context</option>{contexts.map((context) => <option key={context.name} value={context.name}>{context.name}</option>)}</select><ChevronDown size={14} /></label>
}

/** Renders and persists the active namespace selector. */
function NamespacePicker({ context, value, onChange }: { context: string; value: string; onChange: (value: string) => void }) {
  const [namespaces, setNamespaces] = useState<string[]>([])
  useEffect(() => {
    if (!context) return
    const definition = commonDefinitions.find((item) => item.resource === 'namespaces')!
    fetchResources(context, definition, 'all').then((response) => setNamespaces(response.items.map((item) => item.name).sort())).catch(() => setNamespaces([]))
  }, [context])
  return <label className="select-shell namespace-select"><span className="namespace-glyph">ns</span><select value={value} onChange={(event) => onChange(event.target.value)} disabled={!context}><option value="all">All namespaces</option>{namespaces.map((namespace) => <option key={namespace} value={namespace}>{namespace}</option>)}</select><ChevronDown size={14} /></label>
}

/** Reads the current browser URL into the KubeHaggis route model. */
function readRoute(): RouteState {
  const parts = window.location.pathname.split('/').filter(Boolean)
  if (parts[0] !== 'resources' || !parts[1]) return { path: '/' }
  return { path: window.location.pathname, resource: decodeURIComponent(parts[1]), name: parts[2] ? decodeURIComponent(parts[2]) : undefined, namespace: new URLSearchParams(window.location.search).get('namespace') || undefined }
}

/** Reads a string from a resource metadata object. */
function metadataString(object: ResourceObject, field: string): string {
  const metadata = object.metadata
  if (!metadata || typeof metadata !== 'object' || Array.isArray(metadata)) return ''
  const value = (metadata as Record<string, unknown>)[field]
  return typeof value === 'string' ? value : ''
}

/** Reads valid standard owner references from an unstructured resource. */
function readOwnerReferences(object: ResourceObject): ResourceOwnerReference[] {
  const metadata = object.metadata
  if (!metadata || typeof metadata !== 'object' || Array.isArray(metadata)) return []
  const rawReferences = (metadata as Record<string, unknown>).ownerReferences
  if (!Array.isArray(rawReferences)) return []

  return rawReferences.flatMap((rawReference) => {
    if (!rawReference || typeof rawReference !== 'object' || Array.isArray(rawReference)) return []
    const reference = rawReference as Record<string, unknown>
    const apiVersion = typeof reference.apiVersion === 'string' ? reference.apiVersion : ''
    const kind = typeof reference.kind === 'string' ? reference.kind : ''
    const name = typeof reference.name === 'string' ? reference.name : ''
    if (!apiVersion || !kind || !name) return []

    const owner: ResourceOwnerReference = { apiVersion, kind, name }
    if (typeof reference.uid === 'string' && reference.uid) owner.uid = reference.uid
    if (typeof reference.controller === 'boolean') owner.controller = reference.controller
    if (typeof reference.blockOwnerDeletion === 'boolean') owner.blockOwnerDeletion = reference.blockOwnerDeletion
    return [owner]
  })
}

/** Resolves an owner reference to a discovered generic resource definition. */
function resolveOwnerDefinition(owner: ResourceOwnerReference, definitions: ResourceDefinition[]): ResourceDefinition | undefined {
  const separator = owner.apiVersion.indexOf('/')
  const group = separator === -1 ? '' : owner.apiVersion.slice(0, separator)
  const version = separator === -1 ? owner.apiVersion : owner.apiVersion.slice(separator + 1)
  return definitions.find((definition) => definition.group === group && definition.version === version && definition.kind === owner.kind)
    ?? commonDefinitions.find((definition) => definition.group === group && definition.version === version && definition.kind === owner.kind)
}

/** Resolves a resource slug against discovered definitions and common fallbacks. */
function resolveDefinition(resource: string, definitions: ResourceDefinition[]): ResourceDefinition {
  return definitions.find((item) => item.resource === resource) ?? commonDefinitions.find((item) => item.resource === resource) ?? { group: '', version: 'v1', resource, kind: titleCase(resource), namespaced: true, verbs: [] }
}

/** Returns the browser storage key used for one context namespace selection. */
function namespaceKey(context: string): string {
  return `kube-haggis.namespace.${context}`
}

/** Reads a persisted namespace selection with a safe all-namespaces fallback. */
function readNamespace(context: string): string {
  return window.localStorage.getItem(namespaceKey(context)) || 'all'
}

/** Builds a deep-linkable application URL with its cluster scope preserved. */
function routeHref(path: string, context: string, namespace: string): string {
  const params = new URLSearchParams()
  if (context) params.set('context', context)
  params.set('namespace', namespace)
  return `${path}?${params.toString()}`
}

/** Converts a Kubernetes resource slug into a human-readable heading. */
function titleCase(value: string): string {
  return value.replace(/[-_]/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase())
}

/** Produces a compact host representation for dashboard endpoint labels. */
function shortServer(value: string): string {
  try {
    const parsed = new URL(value)
    return `${parsed.hostname}${parsed.port ? `:${parsed.port}` : ''}`
  } catch {
    return value || 'unknown endpoint'
  }
}

/** Returns a human-readable relative age for Kubernetes timestamps. */
function relativeTime(value: string): string {
  const timestamp = new Date(value).getTime()
  if (!timestamp) return 'unknown'
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000))
  if (seconds < 60) return `${seconds}s ago`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
  return `${Math.floor(seconds / 86400)}d ago`
}

/** Masks Secret data fields before they are placed in the browser editor. */
function redactSecret(object: ResourceObject): ResourceObject {
  const copy = sanitizeEditorObject(object)
  for (const field of ['data', 'stringData']) {
    const values = copy[field]
    if (!values || typeof values !== 'object' || Array.isArray(values)) continue
    copy[field] = Object.fromEntries(Object.keys(values as Record<string, unknown>).map((key) => [key, '[redacted]']))
  }
  return copy
}

/** Removes Kubernetes-managed metadata before an object enters the YAML editor. */
function sanitizeEditorObject(object: ResourceObject): ResourceObject {
  const copy = JSON.parse(JSON.stringify(object)) as ResourceObject
  const metadata = copy.metadata
  if (metadata && typeof metadata === 'object' && !Array.isArray(metadata)) {
    delete (metadata as Record<string, unknown>).managedFields
  }
  return copy
}
