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
  Trash2,
  Upload,
  X,
  Zap,
} from 'lucide-react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { parse, stringify } from 'yaml'
import {
  applyResource,
  applyNodeAction,
  applyPVCAction,
  applyWorkloadAction,
  deleteResource,
  deleteResources,
  fetchContexts,
  fetchDiscovery,
  fetchOverview,
  fetchPVCOperations,
  fetchResource,
  fetchResourceSchema,
  fetchResourceEvents,
  fetchRelated,
  fetchResources,
  downloadPodFile,
  launchEphemeralContainer,
  logsUrl,
  portForwardUrl,
  uploadPodFile,
  resourceEventsWatchUrl,
  reloadContexts as reloadKubeconfig,
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
  ResourceDeleteItem,
  ResourceDeleteResponse,
  BulkDeleteResponse,
  ResourceListResponse,
  ResourceObject,
  ResourceSchemaField,
  ResourceSchemaResponse,
  ResourceSummary,
  RelatedAggregateState,
  RelatedResource,
  RelatedResourcesResponse,
  RouteState,
  TerminalMessage,
  MetricsOverview,
  MetricsState,
  NodeAction,
  NodeActionRequest,
  NodeActionResponse,
  NodeTaint,
  NodeMetric,
  PodMetric,
  PVCAction,
  PVCActionRequest,
  PVCActionResponse,
  PVCOperationInfo,
  PVCOperationState,
  WorkloadAction,
  WorkloadActionRequest,
  WorkloadActionResponse,
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
      { label: 'Nodes', resource: 'nodes', icon: Server },
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
  const [contextLoading, setContextLoading] = useState(true)
  const [contextRequestError, setContextRequestError] = useState('')
  const contextRequestID = useRef(0)
  const contextLoadingRef = useRef(true)

  useEffect(() => {
    let active = true

    /** Loads context metadata while preserving the current browser selection after startup. */
    async function loadContexts(initial: boolean) {
      if (!initial && contextLoadingRef.current) return
      const requestID = contextRequestID.current + 1
      contextRequestID.current = requestID
      try {
        const response = await fetchContexts()
        if (!active || requestID !== contextRequestID.current) return
        setContexts(response)
        setContextRequestError(response.reloadError || '')
        if (initial) {
          const search = new URLSearchParams(window.location.search)
          const fromUrl = search.get('context')
          setSelectedContext(fromUrl || response.currentContext)
        }
      } catch (error) {
        if (!active || requestID !== contextRequestID.current) return
        const message = (error as Error).message
        if (initial) {
          setContexts({ currentContext: '', contexts: [], sourceState: 'error', revision: 0, reloaded: false, error: message })
        } else {
          setContextRequestError(message)
        }
      } finally {
        if (active && requestID === contextRequestID.current && initial) {
          contextLoadingRef.current = false
          setContextLoading(false)
        }
      }
    }

    void loadContexts(true)
    const interval = window.setInterval(() => void loadContexts(false), 15000)
    return () => {
      active = false
      window.clearInterval(interval)
    }
  }, [])

  const contextRevision = contexts?.revision ?? 0

  useEffect(() => {
    if (!selectedContext) return
    let active = true
    const routeNamespace = new URLSearchParams(window.location.search).get('namespace')
    setSelectedNamespace(routeNamespace || readNamespace(selectedContext))
    setWriteEnabled(false)
    fetchDiscovery(selectedContext)
      .then((nextDefinitions) => {
        if (active && nextDefinitions.length > 0) setDefinitions(nextDefinitions)
      })
      .catch(() => {
        if (active) setDefinitions((currentDefinitions) => currentDefinitions.length ? currentDefinitions : commonDefinitions)
      })
    return () => {
      active = false
    }
  }, [selectedContext, contextRevision])

  /** Forces a kubeconfig reload while preserving the selected context and namespace. */
  async function reloadKubeconfigSources() {
    const requestID = contextRequestID.current + 1
    contextRequestID.current = requestID
    contextLoadingRef.current = true
    setContextLoading(true)
    setContextRequestError('')
    try {
      const response = await reloadKubeconfig()
      if (requestID !== contextRequestID.current) return
      setContexts(response)
      setContextRequestError(response.error || response.reloadError || '')
    } catch (error) {
      if (requestID !== contextRequestID.current) return
      setContextRequestError((error as Error).message)
    } finally {
      if (requestID === contextRequestID.current) {
        contextLoadingRef.current = false
        setContextLoading(false)
      }
    }
  }

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
    <OverviewPage context={selectedContext} contexts={contexts} activeContext={activeContext} revision={contextRevision} />
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
            <NamespacePicker context={selectedContext} value={selectedNamespace} revision={contextRevision} onChange={changeNamespace} />
            <button
              className={`write-switch ${writeEnabled ? 'write-switch-on' : ''}`}
              onClick={() => setWriteEnabled((enabled) => !enabled)}
              disabled={!selectedContext}
            >
              <span className="write-switch-light" />
              {writeEnabled ? 'Write mode on' : 'Read-only'}
            </button>
            <button className="icon-button kubeconfig-reload" onClick={() => void reloadKubeconfigSources()} disabled={contextLoading} aria-label="Reload kubeconfig" title="Reload kubeconfig">
              {contextLoading ? <LoaderCircle size={17} className="spin" /> : <RefreshCw size={17} />}
            </button>
            <button className="icon-button" aria-label="Settings">
              <Settings2 size={18} />
            </button>
          </div>
        </header>
        <div className="page-scroll">
          {contexts && !(contexts.error && contexts.contexts.length === 0) && (contextLoading || contexts.sourceState === 'changed' || Boolean(contexts.reloadError) || Boolean(contextRequestError) || (Boolean(selectedContext) && contexts.contexts.length > 0 && !activeContext)) ? <KubeconfigNotice state={contexts.sourceState} error={contexts.reloadError || contextRequestError} selectedContext={selectedContext} selectedContextAvailable={Boolean(activeContext)} loading={contextLoading} onReload={() => void reloadKubeconfigSources()} /> : null}
          {content}
        </div>
      </main>
    </div>
  )
}

/** Renders the cluster overview and its health summary panels. */
function OverviewPage({ context, contexts, activeContext, revision }: { context: string; contexts: ContextsResponse | null; activeContext?: ContextSummary; revision: number }) {
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
  }, [context, revision])

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

/** Renders safe reload, source-change, and stale-selection notices without replacing active pages. */
function KubeconfigNotice({ state, error, selectedContext, selectedContextAvailable, loading, onReload }: { state: ContextsResponse['sourceState']; error?: string; selectedContext: string; selectedContextAvailable: boolean; loading: boolean; onReload: () => void }) {
  let title = 'Kubeconfig sources changed'
  let detail = 'Contexts and discovery were refreshed. The selected context and any open editor were left untouched.'
  let tone = 'changed'
  if (loading) {
    title = 'Reloading kubeconfig sources'
    detail = 'Refreshing contexts and discovery. The selected context and any open editor will remain in place.'
    tone = 'loading'
  } else if (error || state === 'error') {
    title = 'Kubeconfig reload needs attention'
    detail = 'The last-good context and client state remain active. Fix the source files and reload again.'
    tone = 'error'
  } else if (!selectedContextAvailable && selectedContext) {
    title = 'Selected context is no longer available'
    detail = `"${selectedContext}" remains selected to avoid switching context or discarding active edits. Choose another context above.`
    tone = 'error'
  } else if (state === 'changed') {
    title = 'Kubeconfig sources changed'
    detail = 'Contexts and discovery were refreshed. The selected context and any open editor were left untouched.'
    tone = 'changed'
  }
  return <div className={`kubeconfig-notice kubeconfig-notice-${tone}`} role="status"><span className="kubeconfig-notice-mark">{loading ? <LoaderCircle size={15} className="spin" /> : tone === 'error' ? <AlertTriangle size={15} /> : <RefreshCw size={15} />}</span><div><strong>{title}</strong><span>{detail}</span>{error && <code>{error}</code>}</div>{!loading && <button type="button" className="secondary-button" onClick={onReload}><RefreshCw size={14} /> Reload</button>}</div>
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
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [selectedKeys, setSelectedKeys] = useState<string[]>([])
  const [bulkReview, setBulkReview] = useState<BulkDeleteResponse | null>(null)
  const [bulkReviewSelection, setBulkReviewSelection] = useState<string[]>([])
  const [bulkResults, setBulkResults] = useState<BulkDeleteResponse | null>(null)
  const [bulkConfirmed, setBulkConfirmed] = useState(false)
  const [bulkConfirmation, setBulkConfirmation] = useState('')
  const [bulkBusy, setBulkBusy] = useState<'review' | 'apply' | ''>('')
  const [bulkError, setBulkError] = useState('')

  useEffect(() => {
    let active = true
    let stream: EventSource | undefined
    setBulkReview(null)
    setBulkReviewSelection([])
    setBulkConfirmed(false)
    setBulkConfirmation('')
    const load = () => {
      setLoading(true)
      fetchResources(context, definition, namespace, labelSelector)
        .then((data) => {
          if (!active) return
          setResponse(data)
          setSelectedKeys((current) => current.filter((key) => data.items.some((item) => resourceItemKey(item) === key)))
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
  }, [context, definition, namespace, labelSelector, refreshNonce])

  if (!context) return <SetupState />
  const filtered = response?.items.filter((item) => item.name.toLowerCase().includes(search.toLowerCase())) ?? []
  const selected = response?.items.filter((item) => selectedKeys.includes(resourceItemKey(item))) ?? []
  const allFilteredSelected = filtered.length > 0 && filtered.every((item) => selectedKeys.includes(resourceItemKey(item)))
  const selectionLocked = Boolean(bulkReview) || Boolean(bulkBusy)

  /** Clears a bulk review and any stale result when the selection changes. */
  function clearBulkReview() {
    setBulkReview(null)
    setBulkReviewSelection([])
    setBulkConfirmed(false)
    setBulkConfirmation('')
    setBulkError('')
  }

  /** Changes one row selection and invalidates any older destructive review. */
  function toggleSelection(item: ResourceSummary, checked: boolean) {
    if (selectionLocked) return
    clearBulkReview()
    const key = resourceItemKey(item)
    setSelectedKeys((current) => {
      const next = new Set(current)
      if (checked) next.add(key)
      else next.delete(key)
      return [...next]
    })
    setBulkResults(null)
  }

  /** Selects or clears the currently filtered rows without selecting hidden rows. */
  function toggleFilteredSelection(checked: boolean) {
    if (selectionLocked) return
    clearBulkReview()
    setSelectedKeys((current) => {
      const next = new Set(current)
      filtered.forEach((item) => {
        if (checked) next.add(resourceItemKey(item))
        else next.delete(resourceItemKey(item))
      })
      return [...next]
    })
    setBulkResults(null)
  }

  /** Runs a Kubernetes dry-run for the exact selected resource versions. */
  async function reviewBulkDelete() {
    if (!writeEnabled) {
      setBulkError('Enable write mode before reviewing a bulk delete.')
      return
    }
    if (!selected.length) {
      setBulkError('Select at least one resource before reviewing a bulk delete.')
      return
    }
    if (selected.some((item) => !item.resourceVersion)) {
      setBulkError('Every selected resource needs a resource version. Refresh the list and select it again.')
      return
    }
    setBulkBusy('review')
    setBulkError('')
    setBulkResults(null)
    try {
      const result = await deleteResources(context, definition, namespace, selected.map(resourceDeleteItem), true, false, '')
      setBulkReview(result)
      setBulkReviewSelection(selected.map(resourceDeleteReviewKey).sort())
      setBulkConfirmed(false)
      setBulkConfirmation('')
    } catch (reviewError) {
      setBulkError((reviewError as Error).message)
    } finally {
      setBulkBusy('')
    }
  }

  /** Applies only the selection and resource versions that passed the bulk dry-run. */
  async function applyReviewedBulkDelete() {
    if (!writeEnabled) {
      setBulkError('Enable write mode before applying a bulk delete.')
      return
    }
    if (!bulkReview || bulkReview.status !== 'dry-run passed') {
      setBulkError('Resolve every dry-run error before applying this bulk delete.')
      return
    }
    const currentSelection = selected.map(resourceDeleteReviewKey).sort()
    if (currentSelection.length !== bulkReviewSelection.length || currentSelection.some((key, index) => key !== bulkReviewSelection[index])) {
      setBulkReview(null)
      setBulkReviewSelection([])
      setBulkConfirmed(false)
      setBulkConfirmation('')
      setBulkError('The selection or a resource version changed after review. Refresh the list and review the delete again.')
      return
    }
    const expectedConfirmation = bulkDeleteConfirmation(selected.length)
    if (!bulkConfirmed || bulkConfirmation.trim() !== expectedConfirmation) {
      setBulkError(`Type ${expectedConfirmation} and check the confirmation box before deleting.`)
      return
    }
    setBulkBusy('apply')
    setBulkError('')
    try {
      const result = await deleteResources(context, definition, namespace, selected.map(resourceDeleteItem), false, true, bulkConfirmation.trim())
      setBulkResults(result)
      setBulkReview(null)
      setBulkReviewSelection([])
      setBulkConfirmed(false)
      setBulkConfirmation('')
      setSelectedKeys((current) => current.filter((key) => !result.items.some((item) => item.success && resourceItemKey(item) === key)))
      setRefreshNonce((value) => value + 1)
    } catch (applyError) {
      setBulkError((applyError as Error).message)
    } finally {
      setBulkBusy('')
    }
  }

  return (
    <div className="resource-page">
      <section className="resource-intro">
        <div><div className="eyebrow"><span className="eyebrow-mark" /> Resource collection</div><h1>{titleCase(definition.resource)}</h1><p>{definition.namespaced ? 'Namespaced resource' : 'Cluster-scoped resource'} / {namespace === 'all' || !definition.namespaced ? 'all namespaces' : namespace}</p></div>
        <div className="resource-intro-meta"><span className={watching ? 'live-chip' : 'quiet-chip'}><span />{watching ? 'Live watch' : 'Refresh fallback'}</span><span>{response?.items.length ?? 0} objects</span></div>
      </section>
      <div className="table-toolbar">
        <label className="search-field"><Search size={16} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder={`Search ${definition.resource}`} /><kbd>/</kbd></label>
        <label className="filter-field"><ListFilter size={15} /><input value={labelSelector} onChange={(event) => setLabelSelector(event.target.value)} placeholder="label selector" /></label>
        <button className="icon-button toolbar-action" onClick={() => setRefreshNonce((value) => value + 1)} aria-label="Refresh resources"><RefreshCw size={16} /></button>
        <button className="primary-button" disabled={!writeEnabled} onClick={onCreate} title={writeEnabled ? `Create a ${definition.kind}` : 'Enable write mode to create resources'}><span>+</span> New {definition.kind}</button>
      </div>
      <div className="selection-toolbar">
        <span><strong>{selected.length}</strong> selected / {definition.kind} / {namespace === 'all' || !definition.namespaced ? 'cluster scope' : namespace}</span>
        <div><button type="button" className="secondary-button danger-button" disabled={!writeEnabled || !selected.length || selectionLocked} onClick={() => void reviewBulkDelete()}><Trash2 size={14} /> Review delete{selected.length ? ` (${selected.length})` : ''}</button>{selected.length > 0 && <button type="button" className="text-button" disabled={selectionLocked} onClick={() => { setSelectedKeys([]); clearBulkReview(); setBulkResults(null) }}>Clear selection</button>}</div>
      </div>
      {error ? <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div> : null}
      {bulkError ? <div className="inline-error page-error"><AlertTriangle size={15} />{bulkError}</div> : null}
      {bulkReview && <BulkDeleteReviewPanel definition={definition} namespace={namespace} items={selected} review={bulkReview} confirmed={bulkConfirmed} confirmation={bulkConfirmation} busy={bulkBusy} onConfirmed={setBulkConfirmed} onConfirmation={setBulkConfirmation} onCancel={clearBulkReview} onApply={() => void applyReviewedBulkDelete()} />}
      {bulkResults && <BulkDeleteResults definition={definition} result={bulkResults} />}
      <div className="resource-table-wrap">
        <table className="resource-table"><thead><tr><th><input className="table-checkbox" type="checkbox" checked={allFilteredSelected} disabled={!filtered.length || selectionLocked} aria-label="Select all visible resources" onChange={(event) => toggleFilteredSelection(event.target.checked)} /></th><th>Name</th><th>Namespace</th><th>Status</th><th>Ready</th><th>Age</th><th>Labels</th><th /></tr></thead><tbody>
          {loading && !response ? <tr><td colSpan={8}><div className="table-loading"><LoaderCircle size={18} className="spin" /> Reading API objects</div></td></tr> : null}
          {!loading && !filtered.length ? <tr><td colSpan={8}><div className="table-empty"><Box size={20} /><strong>No matching objects</strong><span>Try a different search or label selector.</span></div></td></tr> : null}
          {filtered.map((item) => <ResourceRow key={resourceItemKey(item)} item={item} selected={selectedKeys.includes(resourceItemKey(item))} selectionDisabled={selectionLocked} onToggle={toggleSelection} onOpen={onOpen} />)}
        </tbody></table>
      </div>
      <div className="table-footer"><span>Showing {filtered.length} of {response?.items.length ?? 0}</span><span><span className="table-footer-dot" /> updates arrive through the Kubernetes watch</span></div>
    </div>
  )
}

/** Renders one row in the generic resource table. */
function ResourceRow({ item, selected, selectionDisabled, onToggle, onOpen }: { item: ResourceSummary; selected: boolean; selectionDisabled: boolean; onToggle: (item: ResourceSummary, checked: boolean) => void; onOpen: (name: string, namespace?: string) => void }) {
  const statusTone = item.status.toLowerCase() === 'true' || item.status.toLowerCase() === 'running' || item.status.toLowerCase() === 'active' ? 'good' : item.status.toLowerCase() === 'failed' || item.status.toLowerCase() === 'false' ? 'warning' : 'neutral'
  const labels = Object.entries(item.labels ?? {}).slice(0, 2)
  return <tr className={selected ? 'resource-row-selected' : ''} onClick={() => onOpen(item.name, item.namespace)}><td><input className="table-checkbox" type="checkbox" checked={selected} disabled={selectionDisabled} aria-label={`Select ${item.name}`} onClick={(event) => event.stopPropagation()} onChange={(event) => onToggle(item, event.target.checked)} /></td><td><button className="resource-name" onClick={(event) => { event.stopPropagation(); onOpen(item.name, item.namespace) }}>{item.name}</button><span className="resource-uid">{item.uid.slice(0, 12)}</span></td><td><span className="namespace-cell">{item.namespace || 'cluster'}</span></td><td><span className={`status-pill status-${statusTone}`}><span />{item.status}</span></td><td><code className="ready-cell">{item.ready || '—'}</code></td><td><span className="age-cell">{relativeTime(item.createdAt)}</span></td><td><div className="label-stack">{labels.map(([key, value]) => <span key={key}>{key}={value}</span>)}{Object.keys(item.labels ?? {}).length > 2 && <span>+{Object.keys(item.labels ?? {}).length - 2}</span>}</div></td><td><button className="icon-button row-menu" aria-label={`Actions for ${item.name}`} onClick={(event) => event.stopPropagation()}><MoreHorizontal size={16} /></button></td></tr>
}

/** Renders the high-friction dry-run and confirmation review for a bulk delete. */
function BulkDeleteReviewPanel({ definition, namespace, items, review, confirmed, confirmation, busy, onConfirmed, onConfirmation, onCancel, onApply }: { definition: ResourceDefinition; namespace: string; items: ResourceSummary[]; review: BulkDeleteResponse; confirmed: boolean; confirmation: string; busy: 'review' | 'apply' | ''; onConfirmed: (confirmed: boolean) => void; onConfirmation: (confirmation: string) => void; onCancel: () => void; onApply: () => void }) {
  const expectedConfirmation = bulkDeleteConfirmation(items.length)
  const ready = review.status === 'dry-run passed'
  const canApply = ready && confirmed && confirmation.trim() === expectedConfirmation && busy !== 'apply'
  const reviewFailures = review.items.filter((item) => !item.success)
  return <div className={`bulk-delete-review ${ready ? '' : 'bulk-delete-review-blocked'}`}>
    <div className="bulk-delete-heading"><span className={ready ? 'review-ready' : 'node-review-blocked-chip'}>{ready ? <><Check size={13} /> Dry-run passed</> : <><AlertTriangle size={13} /> Review blocked</>}</span><strong>Delete {items.length} {definition.kind}{items.length === 1 ? '' : 's'}</strong></div>
    <p>{ready ? 'Kubernetes accepted this exact resource-versioned deletion set during server-side dry-run. No cluster state has changed.' : 'No deletion was submitted. Resolve every listed item error, refresh if needed, and review the full selection again.'}</p>
    <div className="bulk-delete-scope"><span>Resource context</span><strong>{definition.group ? `${definition.group}/${definition.version}` : definition.version} / {definition.resource} / {namespace === 'all' || !definition.namespaced ? 'cluster scope' : namespace}</strong></div>
    <div className="bulk-delete-items"><div className="bulk-delete-items-heading"><span>Selected resources</span><span>{items.length} total</span></div>{items.slice(0, 8).map((item) => <code key={resourceItemKey(item)}>{item.namespace ? `${item.namespace}/` : ''}{item.name} @ rv/{item.resourceVersion}</code>)}{items.length > 8 && <span className="bulk-delete-more">+{items.length - 8} more selected</span>}</div>
    {reviewFailures.length > 0 && <div className="bulk-delete-failures"><strong>Dry-run errors</strong>{reviewFailures.map((item) => <span key={`${item.namespace || ''}/${item.name}`}><AlertTriangle size={12} />{item.namespace ? `${item.namespace}/` : ''}{item.name}: {item.error || 'delete review failed'}</span>)}</div>}
    {ready && <><label className="destructive-confirm"><input type="checkbox" checked={confirmed} onChange={(event) => onConfirmed(event.target.checked)} disabled={busy === 'apply'} /><span>I reviewed every selected resource and understand this permanently deletes {items.length} Kubernetes object{items.length === 1 ? '' : 's'}. There is no undo, and owner/finalizer behavior may affect related resources.</span></label><label className="destructive-confirm-input"><span>Type <strong>{expectedConfirmation}</strong> to authorize the final delete</span><input value={confirmation} onChange={(event) => onConfirmation(event.target.value)} disabled={busy === 'apply'} autoComplete="off" spellCheck={false} /></label></>}
    <div className="bulk-delete-actions"><button type="button" className="secondary-button" onClick={onCancel} disabled={Boolean(busy)}>Change selection</button><button type="button" className="danger-button primary-button" onClick={onApply} disabled={!canApply}>{busy === 'apply' ? <LoaderCircle size={15} className="spin" /> : <Trash2 size={15} />} {busy === 'apply' ? 'Deleting' : `Delete ${items.length} resources`}</button></div>
  </div>
}

/** Renders safe per-item outcomes after a bulk delete attempt. */
function BulkDeleteResults({ definition, result }: { definition: ResourceDefinition; result: BulkDeleteResponse }) {
  return <div className="bulk-delete-results"><div className="bulk-delete-results-heading"><div><span className="eyebrow"><span className="eyebrow-mark" /> Mutation results</span><strong>{titleCase(definition.resource)} / {result.succeeded} succeeded / {result.failed} failed</strong></div><span className={result.failed ? 'status-pill status-warning' : 'status-pill status-good'}><span />{result.status}</span></div><div className="bulk-result-list">{result.items.map((item) => <div className="bulk-result-row" key={`${item.namespace || ''}/${item.name}`}><span className={item.success ? 'bulk-result-mark bulk-result-success' : 'bulk-result-mark bulk-result-failure'}>{item.success ? <Check size={13} /> : <AlertTriangle size={13} />}</span><div><strong>{item.namespace ? `${item.namespace}/` : ''}{item.name}</strong><span>{item.success ? item.status : item.error || 'delete failed'}</span></div></div>)}</div></div>
}

/** Renders the tabbed detail view for one generic resource. */
function ResourceDetailPage({ context, namespace, definition, name, writeEnabled, onBack, onOpenEvent, onOpenResource }: { context: string; namespace: string; definition: ResourceDefinition; name: string; writeEnabled: boolean; onBack: () => void; onOpenEvent: (event: EventSummary) => void; onOpenResource: (definition: ResourceDefinition, name: string, namespace?: string) => void }) {
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
  const schemaCandidate = !isSecret && isCustomResourceDefinition(definition)
  const [resourceSchema, setResourceSchema] = useState<ResourceSchemaResponse | null>(null)
  const [schemaLoading, setSchemaLoading] = useState(false)

  useEffect(() => {
    let active = true
    setTab(isNew && schemaCandidate ? 'form' : 'overview')
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
  }, [context, definition.group, definition.version, definition.resource, definition.kind, definition.namespaced, namespace, name, isNew, isSecret])

  useEffect(() => {
    if (!schemaCandidate) {
      setResourceSchema(null)
      setSchemaLoading(false)
      return
    }
    let active = true
    setResourceSchema(null)
    setSchemaLoading(true)
    fetchResourceSchema(context, definition)
      .then((response) => active && setResourceSchema(response))
      .catch((schemaError: Error) => active && setResourceSchema({
        state: 'error',
        message: schemaError.message,
        group: definition.group,
        version: definition.version,
        resource: definition.resource,
        kind: definition.kind,
      }))
      .finally(() => active && setSchemaLoading(false))
    return () => {
      active = false
    }
  }, [context, definition.group, definition.version, definition.resource, definition.kind, schemaCandidate])

  /** Runs Kubernetes dry-run validation for the edited resource document. */
  async function reviewChanges() {
    if (resourceSchema?.state === 'available' && resourceSchema.schema) {
      const validationErrors = validateSchemaDocument(resourceSchema.schema, object || {})
      if (isNew && !metadataString(object || {}, 'name')) validationErrors.unshift('metadata.name is required for form-created resources; use YAML for generateName.')
      if (isNew && definition.namespaced && namespace === 'all' && !metadataString(object || {}, 'namespace')) validationErrors.unshift('metadata.namespace is required when creating a namespaced resource from All namespaces.')
      if (validationErrors.length) {
        setTab('form')
        setError(`Form validation: ${validationErrors.slice(0, 4).join(' ')}`)
        return
      }
    }
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
      const safeRefreshed = sanitizeEditorObject(refreshed)
      const text = stringify(isSecret ? redactSecret(safeRefreshed) : safeRefreshed)
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

  /** Replaces the generic YAML editor document with one changed through the schema form. */
  function updateFormObject(nextObject: ResourceObject) {
    const safeObject = sanitizeEditorObject(nextObject)
    setObject(safeObject)
    setYamlText(stringify(safeObject))
    setReviewReady(false)
    setError('')
  }

  /** Syncs the in-memory object when an operator switches from valid YAML to the schema form. */
  function syncObjectFromYaml() {
    const parsed = parseResourceObject(yamlText)
    if (parsed) setObject(sanitizeEditorObject(parsed))
  }

  if (error && !object) return <ConnectionState error={error} />
  if (!object) return <LoadingState label={`Opening ${name}`} />
  const dirty = yamlText !== originalYaml
  const baseDetailTabs = definition.resource === 'events' ? ['overview', 'yaml', 'related'] : definition.resource === 'pods' && !isNew ? ['overview', 'debug', 'files', 'yaml', 'events', 'terminal', 'port-forward', 'logs', 'related'] : definition.resource === 'pods' ? ['overview', 'yaml', 'events', 'terminal', 'port-forward', 'logs', 'related'] : ['overview', 'yaml', 'events', 'related']
  const schemaTabs = schemaCandidate ? [baseDetailTabs[0], 'form', ...baseDetailTabs.slice(1)] : baseDetailTabs
  const detailTabs = (targetedWorkloadResource(definition) || targetedNodeResource(definition) || targetedPVCResource(definition)) && !isNew ? [schemaTabs[0], 'operations', ...schemaTabs.slice(1)] : schemaTabs
  const resourceNamespace = definition.namespaced ? metadataString(object, 'namespace') || (namespace === 'all' ? '' : namespace) : ''

  return <div className="detail-page">
    <div className="detail-breadcrumb"><button className="back-link" onClick={onBack}>← {titleCase(definition.resource)}</button><span>/</span><strong>{isNew ? 'new' : name}</strong></div>
    <section className="detail-heading"><div><div className="eyebrow"><span className="eyebrow-mark" /> {isNew ? 'Create resource' : 'Resource detail'}</div><h1>{isNew ? `New ${definition.kind}` : name}</h1><p>{definition.kind} / {namespace === 'all' || !definition.namespaced ? 'cluster-scoped' : namespace}</p></div><div className="detail-actions"><span className="status-pill status-good"><span /> {isNew ? 'draft' : 'loaded'}</span><button className="secondary-button" disabled={!writeEnabled || !dirty || saving} onClick={reviewChanges}><ShieldCheck size={15} /> Review changes</button></div></section>
     {!isNew && <DeleteReviewPanel context={context} namespace={resourceNamespace || namespace} definition={definition} name={name} resourceVersion={metadataString(object, 'resourceVersion')} writeEnabled={writeEnabled} onDeleted={onBack} />}
     <div className="detail-tabs" role="tablist">{detailTabs.map((item) => <button key={item} className={tab === item ? 'detail-tab active' : 'detail-tab'} onClick={() => { if (item === 'form') syncObjectFromYaml(); setTab(item) }}>{item === 'yaml' ? 'YAML editor' : item === 'form' ? 'Schema form' : titleCase(item)}</button>)}</div>
    {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
       {tab === 'overview' && <div className="detail-grid"><div className="panel detail-panel"><PanelHeading eyebrow="Identity" title="Resource coordinates" action="Stable object" tone="good" /><div className="identity-list"><IdentityRow label="Kind" value={definition.kind} /><IdentityRow label="Namespace" value={namespace === 'all' || !definition.namespaced ? 'cluster-scoped' : namespace} /><IdentityRow label="UID" value={String(object.metadata && (object.metadata as Record<string, unknown>).uid || '—')} mono /><IdentityRow label="Resource version" value={String(object.metadata && (object.metadata as Record<string, unknown>).resourceVersion || '—')} mono /></div></div><div className="panel detail-panel"><PanelHeading eyebrow="Fields" title="Top-level shape" action={`${Object.keys(object).length} sections`} tone="neutral" /><div className="field-cloud">{Object.keys(object).map((key) => <span key={key}><span />{key}</span>)}</div></div></div>}
         {tab === 'operations' && targetedWorkloadResource(definition) && <WorkloadActionsPanel context={context} namespace={resourceNamespace || namespace} definition={definition} name={name} object={object} writeEnabled={writeEnabled} onRefresh={refreshObject} />}
         {tab === 'operations' && targetedNodeResource(definition) && <NodeOperationsPanel context={context} name={name} object={object} writeEnabled={writeEnabled} onRefresh={refreshObject} />}
         {tab === 'operations' && targetedPVCResource(definition) && <PVCOperationsPanel context={context} namespace={resourceNamespace || namespace} name={name} object={object} writeEnabled={writeEnabled} onRefresh={refreshObject} />}
     {tab === 'form' && schemaCandidate && <SchemaFormPanel response={resourceSchema} loading={schemaLoading} object={sanitizeEditorObject(object)} isNew={isNew} namespaced={definition.namespaced} namespace={namespace} writeEnabled={writeEnabled} reviewReady={reviewReady} onChange={updateFormObject} onOpenYaml={() => setTab('yaml')} />}
     {tab === 'yaml' && <div className="editor-panel"><div className="editor-toolbar"><div><span className="editor-dot" /><strong>{isSecret ? 'Secret YAML' : 'Resource YAML'}</strong><span>{dirty ? 'unsaved changes' : 'synced with cluster'}</span></div><div>{!writeEnabled && <span className="editor-lock"><LockKeyhole size={13} /> Enable write mode to edit</span>}{reviewReady && <span className="review-ready"><Check size={13} /> Dry-run passed</span>}</div></div>{isSecret && <div className="secret-notice"><LockKeyhole size={15} /><span>{secretsRevealed ? 'Secret values are visible in this editor.' : 'Secret values are masked by default.'}</span>{secretsRevealed ? <button className="reveal-button" onClick={() => { setSecretsRevealed(false); const safeText = stringify(redactSecret(object)); setYamlText(safeText); setOriginalYaml(safeText) }}>Mask values</button> : <button className="reveal-button" onClick={() => { setSecretsRevealed(true); const rawText = stringify(sanitizeEditorObject(object)); setYamlText(rawText); setOriginalYaml(rawText) }}>Reveal values</button>}</div>}<textarea className="yaml-editor" value={yamlText} onChange={(event) => { setYamlText(event.target.value); setReviewReady(false); const parsed = parseResourceObject(event.target.value); if (parsed) setObject(sanitizeEditorObject(parsed)) }} readOnly={!writeEnabled || (isSecret && !secretsRevealed)} spellCheck={false} aria-label="Resource YAML" /><div className="editor-footer"><span><FileCode2 size={14} /> client-side apply / full object update</span>{reviewReady && writeEnabled && <button className="primary-button" onClick={saveChanges} disabled={saving}>{saving ? <LoaderCircle size={15} className="spin" /> : <Check size={15} />} Apply reviewed changes</button>}</div></div>}
      {tab === 'logs' && definition.resource === 'pods' && <PodLogsPanel context={context} namespace={namespace} pod={name} />}
      {tab === 'terminal' && definition.resource === 'pods' && <PodTerminalPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} />}
      {tab === 'port-forward' && definition.resource === 'pods' && <PodPortForwardPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} />}
      {tab === 'debug' && definition.resource === 'pods' && !isNew && <PodDebugPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} writeEnabled={writeEnabled} onRefresh={refreshObject} />}
      {tab === 'files' && definition.resource === 'pods' && !isNew && <PodFileCopyPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} writeEnabled={writeEnabled} />}
      {tab === 'events' && <ResourceEventsPanel context={context} namespace={namespace} uid={metadataString(object, 'uid')} onOpenEvent={onOpenEvent} />}
      {tab === 'related' && <RelatedResourcesPanel context={context} definition={definition} namespace={resourceNamespace} name={name} isNew={isNew} onOpenResource={onOpenResource} />}
  </div>
}

/** Renders a structural CRD form while keeping YAML as the complete editing fallback. */
function SchemaFormPanel({ response, loading, object, isNew, namespaced, namespace, writeEnabled, reviewReady, onChange, onOpenYaml }: { response: ResourceSchemaResponse | null; loading: boolean; object: ResourceObject; isNew: boolean; namespaced: boolean; namespace: string; writeEnabled: boolean; reviewReady: boolean; onChange: (object: ResourceObject) => void; onOpenYaml: () => void }) {
  if (loading && !response) {
    return <div className="schema-form-panel"><div className="schema-form-loading"><LoaderCircle size={18} className="spin" /> Reading structural OpenAPI v3 schema</div></div>
  }
  if (!response || response.state !== 'available' || !response.schema) {
    return <div className="schema-form-panel"><div className="schema-form-header"><div className="logs-title"><FileCode2 size={16} /><strong>Schema form unavailable</strong><span>YAML remains complete</span></div><span className="schema-state-chip schema-state-warning">{schemaStateLabel(response?.state)}</span></div><div className="schema-form-fallback"><AlertTriangle size={18} /><div><strong>{schemaStateTitle(response?.state)}</strong><span>{response?.message || 'The cluster did not provide a usable structural schema for this custom resource.'}</span></div></div><div className="schema-form-footer"><span>Generic CRUD and deep links remain available.</span><button type="button" className="secondary-button" onClick={onOpenYaml}><FileCode2 size={14} /> Open YAML editor</button></div></div>
  }

  const validationErrors = validateSchemaDocument(response.schema, object)
  const metadata = object.metadata && typeof object.metadata === 'object' && !Array.isArray(object.metadata) ? object.metadata as Record<string, unknown> : {}
  const metadataName = typeof metadata.name === 'string' ? metadata.name : ''
  const metadataNamespace = typeof metadata.namespace === 'string' ? metadata.namespace : namespace === 'all' ? '' : namespace
  const updatePath = (path: string[], value: unknown) => onChange(updateSchemaObject(object, path, value))
  return <div className="schema-form-panel">
    <div className="schema-form-header"><div className="logs-title"><FileCode2 size={16} /><strong>{isNew ? 'Create from schema' : 'Edit from schema'}</strong><span>structural OpenAPI v3</span></div><div className="schema-form-header-meta">{reviewReady && <span className="review-ready"><Check size={13} /> Dry-run passed</span>}<span className="schema-state-chip schema-state-good">Form ready</span></div></div>
    <div className="schema-form-intro"><strong>Fields are generated from the cluster CRD.</strong><span>Nested objects, arrays, typed maps, required fields, defaults, enums, descriptions, and common validation constraints are shown here. YAML remains the complete fallback for fields the form does not model.</span></div>
    {!writeEnabled && <div className="operation-lock"><LockKeyhole size={14} /> Enable write mode to edit this form. Review still performs the existing Kubernetes server-side dry-run.</div>}
    <form className="schema-form" onSubmit={(event) => event.preventDefault()}>
      <fieldset className="schema-object schema-metadata" disabled={!writeEnabled}>
        <legend>Metadata</legend>
        <div className="schema-field-grid">
          <label className="schema-field"><span>Name <em>{isNew ? 'required for form create' : 'identity'}</em></span><input value={metadataName} onChange={(event) => updatePath(['metadata', 'name'], event.target.value || undefined)} readOnly={!isNew} required={isNew} /></label>
          {namespaced && <label className="schema-field"><span>Namespace {isNew && namespace === 'all' && <b>required</b>}</span><input value={metadataNamespace} onChange={(event) => updatePath(['metadata', 'namespace'], event.target.value || undefined)} readOnly={!isNew} required={isNew && namespace === 'all'} /></label>}
        </div>
      </fieldset>
      <SchemaFieldEditor field={response.schema} value={object} path={[]} writeEnabled={writeEnabled} onChange={updatePath} />
    </form>
    {validationErrors.length > 0 && <div className="schema-validation"><AlertTriangle size={15} /><div><strong>Form needs attention</strong><span>{validationErrors.slice(0, 4).join(' ')}</span></div></div>}
    <div className="schema-form-footer"><span><ShieldCheck size={14} /> Full-object update / resource-version concurrency</span><button type="button" className="secondary-button" onClick={onOpenYaml}><FileCode2 size={14} /> Use YAML fallback</button></div>
  </div>
}

/** Renders one typed schema field and its nested object, array, or map controls. */
function SchemaFieldEditor({ field, value, path, writeEnabled, onChange }: { field: ResourceSchemaField; value: unknown; path: string[]; writeEnabled: boolean; onChange: (path: string[], value: unknown) => void }) {
  const label = field.title || field.name || (path.length ? 'Value' : 'Spec')
  const present = value !== undefined
  const displayValue = value === undefined ? field.default : value
  const clear = () => onChange(path, undefined)
  if (field.type === 'object') {
    if (!present && field.default === undefined && !field.required && path.length > 0) {
      return <div className="schema-field-add"><div><strong>{label}</strong>{field.description && <span>{field.description}</span>}</div><button type="button" className="secondary-button" disabled={!writeEnabled || field.readOnly} onClick={() => onChange(path, createSchemaValue(field))}>Add section</button></div>
    }
    const objectValue = displayValue && typeof displayValue === 'object' && !Array.isArray(displayValue) ? displayValue as Record<string, unknown> : {}
    return <fieldset className={`schema-object ${path.length ? '' : 'schema-object-root'}`}>
      {path.length > 0 && <legend><span>{label}</span>{field.required && <b>required</b>}{field.readOnly && <em>read-only</em>}{present && !field.required && <button type="button" className="schema-clear" onClick={clear} disabled={!writeEnabled || field.readOnly}>Clear</button>}</legend>}
      {field.description && <p className="schema-description">{field.description}</p>}
      {field.properties?.map((property) => <SchemaFieldEditor key={property.name} field={property} value={readSchemaPath(objectValue, [property.name || ''])} path={[...path, property.name || '']} writeEnabled={writeEnabled && !field.readOnly} onChange={onChange} />)}
      {field.additionalProperties && <SchemaMapEditor field={field.additionalProperties} value={displayValue} path={path} reservedKeys={(field.properties || []).map((property) => property.name || '')} writeEnabled={writeEnabled && !field.readOnly} onChange={onChange} />}
      {!field.properties?.length && !field.additionalProperties && <span className="schema-empty">No editable properties are declared.</span>}
    </fieldset>
  }
  if (field.type === 'array') {
    return <SchemaArrayEditor field={field} value={displayValue} path={path} label={label} present={present} writeEnabled={writeEnabled} onChange={onChange} />
  }

  const enumValues = field.enum || []
  const valueKey = displayValue === undefined ? '' : schemaValueKey(displayValue)
  const stringValue = typeof displayValue === 'string' ? displayValue : displayValue === undefined || displayValue === null ? '' : String(displayValue)
  return <label className={`schema-field schema-field-${field.type}`}>
    <span>{label} {field.required && <b>required</b>}{field.readOnly && <em>read-only</em>}{present && !field.required && <button type="button" className="schema-clear" onClick={(event) => { event.preventDefault(); clear() }} disabled={!writeEnabled || field.readOnly}>Clear</button>}</span>
    {field.description && <small>{field.description}</small>}
    {enumValues.length > 0 ? <select value={valueKey} onChange={(event) => onChange(path, event.target.value ? parseSchemaValue(event.target.value) : undefined)} disabled={!writeEnabled || field.readOnly} required={field.required}><option value="">{field.required ? 'Choose a value' : 'Not set'}</option>{enumValues.map((item) => <option key={schemaValueKey(item)} value={schemaValueKey(item)}>{String(item)}</option>)}</select> : field.type === 'boolean' ? <input type="checkbox" checked={displayValue === true} onChange={(event) => onChange(path, event.target.checked)} disabled={!writeEnabled || field.readOnly} /> : <input type={field.type === 'number' || field.type === 'integer' ? 'number' : 'text'} value={stringValue} onChange={(event) => onChange(path, parseSchemaInput(field, event.target.value))} disabled={!writeEnabled || field.readOnly} required={field.required} min={field.minimum} max={field.maximum} minLength={field.minLength} maxLength={field.maxLength} pattern={field.pattern} step={field.type === 'integer' ? 1 : field.multipleOf || 'any'} inputMode={field.type === 'number' || field.type === 'integer' ? 'decimal' : undefined} />}
    {field.format && <i className="schema-format">format / {field.format}</i>}
  </label>
}

/** Renders array items with bounded add and remove controls. */
function SchemaArrayEditor({ field, value, path, label, present, writeEnabled, onChange }: { field: ResourceSchemaField; value: unknown; path: string[]; label: string; present: boolean; writeEnabled: boolean; onChange: (path: string[], value: unknown) => void }) {
  const items = Array.isArray(value) ? value : []
  const clear = () => onChange(path, undefined)
  return <fieldset className="schema-array"><legend><span>{label}</span>{field.required && <b>required</b>}{field.readOnly && <em>read-only</em>}{present && !field.required && <button type="button" className="schema-clear" onClick={clear} disabled={!writeEnabled || field.readOnly}>Clear</button>}</legend>{field.description && <p className="schema-description">{field.description}</p>}<div className="schema-array-items">{items.map((item, index) => <div className="schema-array-item" key={`${path.join('.')}-${index}`}><div className="schema-array-item-heading"><span>Item {index + 1}</span><button type="button" className="schema-clear" onClick={() => onChange(path, items.filter((_, itemIndex) => itemIndex !== index))} disabled={!writeEnabled || field.readOnly}>Remove</button></div>{field.items ? <SchemaFieldEditor field={field.items} value={item} path={[...path, String(index)]} writeEnabled={writeEnabled && !field.readOnly} onChange={onChange} /> : <span className="schema-empty">This array has no item schema. Use YAML.</span>}</div>)}</div><button type="button" className="secondary-button schema-add-button" onClick={() => onChange(path, [...items, createSchemaValue(field.items)])} disabled={!writeEnabled || field.readOnly || !field.items}>Add item</button></fieldset>
}

/** Renders typed additionalProperties maps as editable key/value rows. */
function SchemaMapEditor({ field, value, path, reservedKeys, writeEnabled, onChange }: { field: ResourceSchemaField; value: unknown; path: string[]; reservedKeys: string[]; writeEnabled: boolean; onChange: (path: string[], value: unknown) => void }) {
  const map = value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
  const entries = Object.entries(map).filter(([key]) => !reservedKeys.includes(key))
  const addEntry = () => {
    let key = 'new-key'
    let suffix = 1
    while (Object.prototype.hasOwnProperty.call(map, key)) key = `new-key-${suffix++}`
    onChange(path, { ...map, [key]: createSchemaValue(field) })
  }
  return <div className="schema-map"><div className="schema-map-heading"><span>Map entries</span><em>values / {field.type}</em></div>{entries.map(([key, item]) => <div className="schema-map-row" key={key}><input value={key} onChange={(event) => { const nextKey = event.target.value; if (!safeSchemaObjectKey(nextKey) || nextKey === key) return; const next = { ...map }; delete next[key]; next[nextKey] = item; onChange(path, next) }} disabled={!writeEnabled} aria-label="Map key" /><div className="schema-map-value"><SchemaFieldEditor field={field} value={item} path={[...path, key]} writeEnabled={writeEnabled} onChange={onChange} /></div><button type="button" className="schema-clear" onClick={() => { const next = { ...map }; delete next[key]; onChange(path, next) }} disabled={!writeEnabled}>Remove</button></div>)}<button type="button" className="secondary-button schema-add-button" onClick={addEntry} disabled={!writeEnabled}>Add map entry</button></div>
}

/** Returns a readable label for an explicit schema response state. */
function schemaStateLabel(state?: ResourceSchemaResponse['state']): string {
  if (state === 'permission-denied') return 'RBAC denied'
  if (state === 'non-structural') return 'Non-structural'
  if (state === 'unsupported') return 'Unsupported'
  if (state === 'missing') return 'Schema missing'
  if (state === 'error') return 'Read failed'
  return state === 'available' ? 'Available' : 'Reading'
}

/** Returns a direct explanation for one schema fallback state. */
function schemaStateTitle(state?: ResourceSchemaResponse['state']): string {
  if (state === 'permission-denied') return 'CRD schema access is denied'
  if (state === 'non-structural') return 'This CRD is not structural'
  if (state === 'unsupported') return 'This schema needs YAML'
  if (state === 'missing') return 'No matching CRD schema'
  if (state === 'error') return 'CRD schema could not be read'
  return 'Schema form is loading'
}

/** Returns a safe immutable update of one resource object path. */
function updateSchemaObject(object: ResourceObject, path: string[], value: unknown): ResourceObject {
  const copy = JSON.parse(JSON.stringify(object)) as ResourceObject
  if (!path.length) return value && typeof value === 'object' && !Array.isArray(value) ? value as ResourceObject : copy
  if (path.some((segment) => !safeSchemaObjectKey(segment))) return copy
  let cursor: unknown = copy
  for (let index = 0; index < path.length - 1; index += 1) {
    const segment = path[index]
    const nextSegment = path[index + 1]
    if (Array.isArray(cursor)) {
      const itemIndex = Number(segment)
      if (!Number.isInteger(itemIndex) || itemIndex < 0) return copy
      if (cursor[itemIndex] === undefined) cursor[itemIndex] = /^\d+$/.test(nextSegment) ? [] : {}
      cursor = cursor[itemIndex]
      continue
    }
    if (!cursor || typeof cursor !== 'object') return copy
    const record = cursor as Record<string, unknown>
    if (record[segment] === undefined) record[segment] = /^\d+$/.test(nextSegment) ? [] : {}
    cursor = record[segment]
  }
  const last = path[path.length - 1]
  if (Array.isArray(cursor)) {
    const itemIndex = Number(last)
    if (!Number.isInteger(itemIndex) || itemIndex < 0) return copy
    if (value === undefined) cursor.splice(itemIndex, 1)
    else cursor[itemIndex] = value
  } else if (cursor && typeof cursor === 'object') {
    if (value === undefined) delete (cursor as Record<string, unknown>)[last]
    else (cursor as Record<string, unknown>)[last] = value
  }
  return copy
}

/** Reads a safe nested value from an object or array path. */
function readSchemaPath(object: unknown, path: string[]): unknown {
  let value = object
  for (const segment of path) {
    if (!safeSchemaObjectKey(segment) || value === null || typeof value !== 'object') return undefined
    if (Array.isArray(value)) {
      const index = Number(segment)
      if (!Number.isInteger(index) || index < 0) return undefined
      value = value[index]
    } else {
      value = (value as Record<string, unknown>)[segment]
    }
  }
  return value
}

/** Creates an initial value for a schema field when an operator adds it. */
function createSchemaValue(field?: ResourceSchemaField): unknown {
  if (!field) return ''
  if (field.default !== undefined) return JSON.parse(JSON.stringify(field.default))
  switch (field.type) {
    case 'object':
      return {}
    case 'array':
      return []
    case 'boolean':
      return false
    case 'integer':
    case 'number':
      return 0
    default:
      return ''
  }
}

/** Converts one primitive input string into the JSON value described by a field. */
function parseSchemaInput(field: ResourceSchemaField, value: string): unknown {
  if (value === '') return undefined
  if (field.type === 'integer' || field.type === 'number') return Number(value)
  return value
}

/** Encodes one enum or select value without losing booleans and numbers. */
function schemaValueKey(value: unknown): string {
  return JSON.stringify(value) ?? ''
}

/** Parses one schema select value that was encoded as JSON. */
function parseSchemaValue(value: string): unknown {
  try {
    return JSON.parse(value)
  } catch {
    return value
  }
}

/** Rejects object keys that could mutate browser prototypes during form edits. */
function safeSchemaObjectKey(value: string): boolean {
  return value !== '__proto__' && value !== 'prototype' && value !== 'constructor' && value.length > 0 && !/[\u0000\r\n]/.test(value)
}

/** Validates the schema-visible portion of one resource before Kubernetes dry-run. */
function validateSchemaDocument(field: ResourceSchemaField, object: ResourceObject): string[] {
  return validateSchemaValue(field, object, '$')
}

/** Recursively validates supported schema values and reports actionable paths. */
function validateSchemaValue(field: ResourceSchemaField, value: unknown, path: string): string[] {
  if (value === undefined) return field.default !== undefined || !field.required ? [] : [`${path} is required.`]
  const errors: string[] = []
  if (field.enum?.length && !field.enum.some((candidate) => schemaValuesEqual(candidate, value))) errors.push(`${path} must match one of the listed enum values.`)
  switch (field.type) {
    case 'string':
      if (typeof value !== 'string') errors.push(`${path} must be a string.`)
      else {
        if (field.minLength !== undefined && value.length < field.minLength) errors.push(`${path} must contain at least ${field.minLength} characters.`)
        if (field.maxLength !== undefined && value.length > field.maxLength) errors.push(`${path} must contain at most ${field.maxLength} characters.`)
        if (field.pattern) {
          try {
            if (!new RegExp(field.pattern).test(value)) errors.push(`${path} does not match the required pattern.`)
          } catch {
            errors.push(`${path} uses an invalid validation pattern.`)
          }
        }
      }
      break
    case 'integer':
    case 'number': {
      if (typeof value !== 'number' || !Number.isFinite(value) || (field.type === 'integer' && !Number.isInteger(value))) {
        errors.push(`${path} must be a ${field.type}.`)
        break
      }
      if (field.minimum !== undefined && (field.exclusiveMinimum ? value <= field.minimum : value < field.minimum)) errors.push(`${path} is below the minimum.`)
      if (field.maximum !== undefined && (field.exclusiveMaximum ? value >= field.maximum : value > field.maximum)) errors.push(`${path} is above the maximum.`)
      if (field.multipleOf !== undefined && Math.abs(value / field.multipleOf - Math.round(value / field.multipleOf)) > 1e-9) errors.push(`${path} must be a multiple of ${field.multipleOf}.`)
      break
    }
    case 'boolean':
      if (typeof value !== 'boolean') errors.push(`${path} must be a boolean.`)
      break
    case 'object': {
      if (value === null || typeof value !== 'object' || Array.isArray(value)) {
        errors.push(`${path} must be an object.`)
        break
      }
      const record = value as Record<string, unknown>
      for (const property of field.properties || []) errors.push(...validateSchemaValue(property, record[property.name || ''], `${path}.${property.name || 'field'}`))
      if (field.additionalProperties) {
        const declared = new Set((field.properties || []).map((property) => property.name || ''))
        for (const [key, item] of Object.entries(record)) if (!declared.has(key)) errors.push(...validateSchemaValue(field.additionalProperties, item, `${path}.${key}`))
      }
      break
    }
    case 'array':
      if (!Array.isArray(value)) {
        errors.push(`${path} must be an array.`)
        break
      }
      if (field.minItems !== undefined && value.length < field.minItems) errors.push(`${path} must contain at least ${field.minItems} item${field.minItems === 1 ? '' : 's'}.`)
      if (field.maxItems !== undefined && value.length > field.maxItems) errors.push(`${path} must contain at most ${field.maxItems} items.`)
      if (field.uniqueItems && new Set(value.map((item) => JSON.stringify(item))).size !== value.length) errors.push(`${path} must contain unique items.`)
      if (field.items) value.forEach((item, index) => errors.push(...validateSchemaValue(field.items!, item, `${path}[${index}]`)))
      break
  }
  return errors
}

/** Compares JSON-compatible schema values for enum and uniqueness validation. */
function schemaValuesEqual(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right)
}

/** Parses a YAML editor document only when it is a plain resource object. */
function parseResourceObject(value: string): ResourceObject | null {
  try {
    const parsed = parse(value) as unknown
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as ResourceObject : null
  } catch {
    return null
  }
}

/** Renders a version-aware dry-run and exact-name confirmation flow for one delete. */
function DeleteReviewPanel({ context, namespace, definition, name, resourceVersion, writeEnabled, onDeleted }: { context: string; namespace: string; definition: ResourceDefinition; name: string; resourceVersion: string; writeEnabled: boolean; onDeleted: () => void }) {
  const [review, setReview] = useState<ResourceDeleteResponse | null>(null)
  const [confirmed, setConfirmed] = useState(false)
  const [confirmationText, setConfirmationText] = useState('')
  const [busy, setBusy] = useState<'review' | 'apply' | ''>('')
  const [error, setError] = useState('')

  /** Runs the server-side Kubernetes dry-run before exposing final delete controls. */
  async function reviewDelete() {
    if (!writeEnabled) {
      setError('Enable write mode before reviewing a delete.')
      return
    }
    if (!resourceVersion) {
      setError('This resource has no resource version. Refresh it before reviewing a delete.')
      return
    }
    setBusy('review')
    setError('')
    try {
      const result = await deleteResource(context, definition, namespace, name, resourceVersion, true, false, '')
      setReview(result)
      setConfirmed(false)
      setConfirmationText('')
    } catch (reviewError) {
      setError((reviewError as Error).message)
    } finally {
      setBusy('')
    }
  }

  /** Applies only the reviewed resource version after both confirmation controls pass. */
  async function applyReviewedDelete() {
    if (!writeEnabled) {
      setError('Enable write mode before applying a delete.')
      return
    }
    if (!review || review.status !== 'dry-run passed') {
      setError('Run the server-side dry-run before applying this delete.')
      return
    }
    if (!confirmed || confirmationText.trim() !== name) {
      setError(`Check the confirmation box and type the exact resource name, ${name}.`)
      return
    }
    setBusy('apply')
    setError('')
    try {
      await deleteResource(context, definition, namespace, name, resourceVersion, false, true, confirmationText.trim())
      onDeleted()
    } catch (applyError) {
      setError((applyError as Error).message)
    } finally {
      setBusy('')
    }
  }

  const ready = review?.status === 'dry-run passed'
  const canApply = Boolean(ready && confirmed && confirmationText.trim() === name && busy !== 'apply')
  return <div className="delete-review-panel">
    <div className="delete-review-header"><div className="logs-title"><Trash2 size={16} /><strong>Delete resource</strong><span>destructive action</span></div><span className="delete-review-scope">review then delete</span></div>
    <div className="delete-review-intro"><strong>{definition.kind} / {namespace || 'cluster scope'} / {name}</strong><span>Deletion is permanent. KubeHaggis requires this exact resource version to pass a Kubernetes server-side dry-run before the final request.</span></div>
    {!writeEnabled && <div className="operation-lock"><LockKeyhole size={14} /> Enable write mode to review and delete. Kubernetes RBAC still authorizes the final request.</div>}
    {!review && <div className="delete-review-start"><span>No cluster state changes during review. The final delete is a separate, confirmed request.</span><button type="button" className="danger-button primary-button" disabled={!writeEnabled || Boolean(busy)} onClick={() => void reviewDelete()}>{busy === 'review' ? <LoaderCircle size={15} className="spin" /> : <ShieldCheck size={15} />} {busy === 'review' ? 'Reviewing delete' : 'Review delete'}</button></div>}
    {review && <div className={`delete-review ${ready ? '' : 'delete-review-blocked'}`}><div className="delete-review-heading"><span className={ready ? 'review-ready' : 'node-review-blocked-chip'}>{ready ? <><Check size={13} /> Dry-run passed</> : <><AlertTriangle size={13} /> Review blocked</>}</span><strong>{ready ? 'Delete is ready for final confirmation' : 'Delete cannot proceed'}</strong></div><p>{ready ? 'Kubernetes accepted this exact resource-versioned delete during server-side dry-run. No cluster state has changed.' : 'No deletion was submitted. Refresh the resource and resolve the review failure before retrying.'}</p>{ready && <><label className="destructive-confirm"><input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} disabled={busy === 'apply'} /><span>I reviewed this exact resource and understand Kubernetes will permanently delete it. Related resources may be affected by owner references or finalizers.</span></label><label className="destructive-confirm-input"><span>Type the exact resource name to authorize deletion: <strong>{name}</strong></span><input value={confirmationText} onChange={(event) => setConfirmationText(event.target.value)} disabled={busy === 'apply'} autoComplete="off" spellCheck={false} /></label></>}
      <div className="delete-review-actions"><button type="button" className="secondary-button" onClick={() => { setReview(null); setConfirmed(false); setConfirmationText(''); setError('') }} disabled={Boolean(busy)}>Change review</button><button type="button" className="danger-button primary-button" onClick={() => void applyReviewedDelete()} disabled={!canApply}>{busy === 'apply' ? <LoaderCircle size={15} className="spin" /> : <Trash2 size={15} />} {busy === 'apply' ? 'Deleting' : 'Confirm and delete'}</button></div></div>}
    {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
  </div>
}

/** Renders discovery-backed relationships and explicit read limitations for a resource. */
function RelatedResourcesPanel({ context, definition, namespace, name, isNew, onOpenResource }: { context: string; definition: ResourceDefinition; namespace: string; name: string; isNew: boolean; onOpenResource: (definition: ResourceDefinition, name: string, namespace?: string) => void }) {
  const [response, setResponse] = useState<RelatedResourcesResponse | null>(null)
  const [loading, setLoading] = useState(!isNew)
  const [error, setError] = useState('')

  useEffect(() => {
    if (isNew) {
      setLoading(false)
      return
    }
    let active = true
    setLoading(true)
    fetchRelated(context, definition, namespace, name)
      .then((data) => {
        if (!active) return
        setResponse(data)
        setError('')
      })
      .catch((loadError: Error) => active && setError(loadError.message))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [context, definition, namespace, name, isNew])

  if (isNew) return <QuietState icon={<Network size={22} />} title="Save before reading relationships" detail="A new resource has no Kubernetes identity or native relationships until it is created." />
  if (loading && !response) return <div className="relationship-panel"><div className="panel relationship-summary-panel"><div className="relationship-loading"><LoaderCircle size={17} className="spin" /> Reading native relationships</div></div></div>
  if (error && !response) return <div className="relationship-panel"><div className="panel relationship-summary-panel"><PanelHeading eyebrow="Relationships" title="Related resources" action="Read failed" tone="warning" /><div className="inline-error"><AlertTriangle size={15} />{error}</div></div></div>

  const items = response?.items ?? []
  const notices = response?.notices ?? []
  return <div className="relationship-panel">
    <div className="panel relationship-summary-panel">
      <PanelHeading eyebrow="Relationships" title="Related resources" action={relatedAggregateLabel(response?.state || 'empty', items.length)} tone={relatedAggregateTone(response?.state || 'empty')} />
      <p className="relationship-copy">Links are derived from Kubernetes owner metadata, native selectors, binding fields, and explicit PodSpec references. Only sanitized resource coordinates are returned; Secret values are never read into this view.</p>
      {error && <div className="inline-error"><AlertTriangle size={15} />{error}</div>}
    </div>
    {notices.length ? <div className="relationship-notices">{notices.map((notice, index) => <RelatedNotice key={`${notice.type}-${notice.source}-${index}`} notice={notice} />)}</div> : null}
    {items.length ? <div className="relationship-list">{items.map((item, index) => <RelatedResourceRow key={`${item.type}-${item.source}-${item.namespace || ''}-${item.name || ''}-${item.uid || index}`} item={item} onOpenResource={onOpenResource} />)}</div> : notices.length ? <div className="relationship-limited"><AlertTriangle size={16} /><span>No resolved links are visible. The source states above explain what Kubernetes allowed this identity to read.</span></div> : <QuietState icon={<Network size={22} />} title="No related resources" detail={response?.message || 'No safe native relationship was found for this object.'} />}
  </div>
}

/** Describes one action option shown by a targeted workload form. */
type WorkloadActionOption = {
  action: WorkloadAction
  label: string
  detail: string
}

/** Renders a two-step dry-run and confirmation form for built-in workloads. */
function WorkloadActionsPanel({ context, namespace, definition, name, object, writeEnabled, onRefresh }: { context: string; namespace: string; definition: ResourceDefinition; name: string; object: ResourceObject; writeEnabled: boolean; onRefresh: () => Promise<void> }) {
  const options = workloadActionOptions(definition.resource)
  const [action, setAction] = useState<WorkloadAction>(options[0]?.action || 'restart')
  const [replicas, setReplicas] = useState(workloadReplicaCount(object))
  const [container, setContainer] = useState(workloadContainerNames(definition.resource, object)[0] || '')
  const [image, setImage] = useState(workloadContainerImage(definition.resource, object, container))
  const [revision, setRevision] = useState('')
  const [schedule, setSchedule] = useState(workloadSchedule(object))
  const [review, setReview] = useState<WorkloadActionResponse | null>(null)
  const [confirmed, setConfirmed] = useState(false)
  const [busy, setBusy] = useState<'review' | 'apply' | ''>('')
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const containers = workloadContainerNames(definition.resource, object)
  const selectedOption = options.find((option) => option.action === action) || options[0]

  useEffect(() => {
    const nextOptions = workloadActionOptions(definition.resource)
    if (!nextOptions.some((option) => option.action === action)) setAction(nextOptions[0]?.action || 'restart')
  }, [definition.resource])

  useEffect(() => {
    const nextContainers = workloadContainerNames(definition.resource, object)
    const nextContainer = nextContainers.includes(container) ? container : nextContainers[0] || ''
    setContainer(nextContainer)
    setImage(workloadContainerImage(definition.resource, object, nextContainer))
    setReplicas(workloadReplicaCount(object))
    setSchedule(workloadSchedule(object))
    setReview(null)
    setConfirmed(false)
  }, [object])

  /** Clears a review so the operator can change the requested action. */
  function editAction() {
    setReview(null)
    setConfirmed(false)
    setError('')
  }

  /** Changes the selected workload action and invalidates an older dry-run review. */
  function changeAction(value: WorkloadAction) {
    setAction(value)
    setReview(null)
    setConfirmed(false)
    setError('')
    setSuccess('')
  }

  /** Builds and validates the explicit payload sent to the workload action endpoint. */
  function buildWorkloadRequest(dryRun: boolean): WorkloadActionRequest | null {
    const expectedResourceVersion = metadataString(object, 'resourceVersion')
    const request: WorkloadActionRequest = { action, dryRun, expectedResourceVersion }
    if (action === 'scale') {
      if (!Number.isInteger(replicas) || replicas < 0) {
        setError('Replica count must be a whole number of zero or greater.')
        return null
      }
      request.replicas = replicas
    }
    if (action === 'image') {
      if (!container) {
        setError('Choose a container before changing its image.')
        return null
      }
      if (!image.trim()) {
        setError('An image reference is required.')
        return null
      }
      request.container = container
      request.image = image.trim()
    }
    if (action === 'rollback' && revision.trim()) {
      const parsedRevision = Number(revision)
      if (!Number.isInteger(parsedRevision) || parsedRevision < 1) {
        setError('Rollback revision must be a positive whole number, or blank for the previous revision.')
        return null
      }
      request.revision = parsedRevision
    }
    if (action === 'schedule') {
      if (!schedule.trim()) {
        setError('A CronJob schedule is required.')
        return null
      }
      request.schedule = schedule.trim()
    }
    return request
  }

  /** Runs the Kubernetes server-side dry-run used as the review gate. */
  async function reviewAction() {
    if (!writeEnabled) {
      setError('Enable write mode before reviewing a workload action.')
      return
    }
    const request = buildWorkloadRequest(true)
    if (!request) return
    setBusy('review')
    setError('')
    setSuccess('')
    try {
      const result = await applyWorkloadAction(context, definition, namespace, name, request)
      setReview(result)
      setConfirmed(false)
    } catch (reviewError) {
      setError((reviewError as Error).message)
    } finally {
      setBusy('')
    }
  }

  /** Applies only the exact action and resource version that passed review. */
  async function applyReviewedAction() {
    if (!writeEnabled) {
      setError('Enable write mode before applying a workload action.')
      return
    }
    if (!review) {
      setError('Run the server-side dry-run before applying this action.')
      return
    }
    if (!confirmed) {
      setError('Confirm that you reviewed the dry-run before applying this action.')
      return
    }
    const request = buildWorkloadRequest(false)
    if (!request) return
    setBusy('apply')
    setError('')
    setSuccess('')
    try {
      const result = await applyWorkloadAction(context, definition, namespace, name, request)
      const created = result.createdName ? ` / created ${result.createdName}` : ''
      setSuccess(`${workloadActionLabel(action)} applied${created}.`)
      setReview(null)
      setConfirmed(false)
      try {
        await onRefresh()
      } catch (refreshError) {
        setError(`Action applied, but the workload refresh failed: ${(refreshError as Error).message}`)
      }
    } catch (applyError) {
      setError((applyError as Error).message)
    } finally {
      setBusy('')
    }
  }

  const disabled = !writeEnabled || Boolean(busy) || Boolean(review)
  return <div className="workload-operation-panel">
    <div className="workload-operation-header"><div className="logs-title"><RefreshCw size={16} /><strong>Workload operations</strong><span>{definition.kind} native actions</span></div><span className="workload-operation-scope">review then apply</span></div>
    <div className="workload-operation-intro"><strong>Change the running workload without leaving its resource detail.</strong><span>Each action reads the current object, preserves its resource version, and passes through a Kubernetes server-side dry-run before the final write.</span></div>
    {!writeEnabled && <div className="operation-lock"><LockKeyhole size={14} /> Enable write mode to review and apply workload actions. Kubernetes RBAC still authorizes the final operation.</div>}
    <form className="workload-operation-form" onSubmit={(event) => { event.preventDefault(); void reviewAction() }}>
      <div className="workload-action-picker"><label className="workload-operation-field"><span>Action</span><select value={action} onChange={(event) => changeAction(event.target.value as WorkloadAction)} disabled={disabled}>{options.map((option) => <option key={option.action} value={option.action}>{option.label}</option>)}</select></label><div className="workload-action-detail">{selectedOption?.detail}</div></div>
      {action === 'scale' && <label className="workload-operation-field"><span>Desired replicas</span><input type="number" min="0" step="1" value={replicas} onChange={(event) => setReplicas(Number(event.target.value))} disabled={disabled} /></label>}
      {action === 'image' && <div className="workload-operation-grid"><label className="workload-operation-field"><span>Container</span><select value={container} onChange={(event) => { setContainer(event.target.value); setImage(workloadContainerImage(definition.resource, object, event.target.value)) }} disabled={disabled}><option value="">Choose container</option>{containers.map((item) => <option key={item} value={item}>{item}</option>)}</select></label><label className="workload-operation-field"><span>New image</span><input value={image} onChange={(event) => setImage(event.target.value)} disabled={disabled} placeholder="registry.example/app:tag" /></label></div>}
      {action === 'rollback' && <label className="workload-operation-field"><span>Revision <em>blank = previous</em></span><input type="number" min="1" step="1" value={revision} onChange={(event) => setRevision(event.target.value)} disabled={disabled} placeholder="previous" /></label>}
      {action === 'schedule' && <label className="workload-operation-field"><span>Cron schedule</span><input value={schedule} onChange={(event) => setSchedule(event.target.value)} disabled={disabled} placeholder="*/15 * * * *" /></label>}
      <div className="workload-operation-actions"><span>{selectedOption?.detail}</span><button type="submit" className="primary-button" disabled={!writeEnabled || Boolean(busy) || Boolean(review)}>{busy === 'review' ? <LoaderCircle size={15} className="spin" /> : <ShieldCheck size={15} />} {busy === 'review' ? 'Reviewing' : 'Review action'}</button></div>
    </form>
    {review && <div className="workload-review"><div className="workload-review-heading"><span className="review-ready"><Check size={13} /> Dry-run passed</span><strong>{workloadActionLabel(action)} is ready</strong></div><p>Kubernetes accepted this exact resource-versioned operation for server-side dry-run. No cluster state has changed; the final request can still change live workload behavior.</p><label className="workload-confirm"><input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} disabled={busy === 'apply'} /><span>I reviewed the exact action and authorize the final request to change workload state.</span></label><div className="workload-review-actions"><button type="button" className="secondary-button" onClick={editAction} disabled={Boolean(busy)}>Change action</button><button type="button" className="primary-button" onClick={() => void applyReviewedAction()} disabled={busy === 'apply' || !confirmed}>{busy === 'apply' ? <LoaderCircle size={15} className="spin" /> : <Check size={15} />} {busy === 'apply' ? 'Applying' : 'Confirm and apply'}</button></div></div>}
    {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
    {success && <div className="operation-success"><Check size={15} />{success}</div>}
    <div className="workload-operation-footer"><span><ShieldCheck size={14} /> Native dynamic update / subresource</span><span>RBAC denials, conflicts, and unsupported APIs stay visible.</span></div>
  </div>
}

/** Renders a two-step PVC resize and VolumeSnapshot workflow with capability states. */
function PVCOperationsPanel({ context, namespace, name, object, writeEnabled, onRefresh }: { context: string; namespace: string; name: string; object: ResourceObject; writeEnabled: boolean; onRefresh: () => Promise<void> }) {
  const [info, setInfo] = useState<PVCOperationInfo | null>(null)
  const [action, setAction] = useState<PVCAction>('resize')
  const [storage, setStorage] = useState(pvcRequestedStorage(object))
  const [snapshotName, setSnapshotName] = useState('')
  const [snapshotClassName, setSnapshotClassName] = useState('')
  const [review, setReview] = useState<PVCActionResponse | null>(null)
  const [confirmed, setConfirmed] = useState(false)
  const [busy, setBusy] = useState<'review' | 'apply' | ''>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  useEffect(() => {
    let active = true
    setLoading(true)
    setError('')
    setInfo(null)
    setReview(null)
    setConfirmed(false)
    setStorage(pvcRequestedStorage(object))
    setSnapshotName('')
    fetchPVCOperations(context, namespace, name)
      .then((response) => {
        if (!active) return
        setInfo(response)
        setSnapshotClassName((current) => response.snapshot.classes.some((item) => item.name === current) ? current : response.snapshot.classes[0]?.name || '')
      })
      .catch((loadError: Error) => active && setError(loadError.message))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [context, namespace, name, object])

  /** Clears an older review when the operator changes the native PVC operation. */
  function changeAction(value: PVCAction) {
    setAction(value)
    setReview(null)
    setConfirmed(false)
    setError('')
    setSuccess('')
  }

  /** Builds the explicit, resource-versioned request sent to the PVC operation endpoint. */
  function buildPVCRequest(dryRun: boolean, isConfirmed: boolean): PVCActionRequest | null {
    const expectedResourceVersion = metadataString(object, 'resourceVersion')
    if (!expectedResourceVersion) {
      setError('The PVC has no resource version. Refresh it before starting a storage operation.')
      return null
    }
    const request: PVCActionRequest = { action, dryRun, confirmed: isConfirmed, expectedResourceVersion }
    if (action === 'resize') {
      if (info?.resize.state !== 'available') {
        setError(info?.resize.message || 'PVC expansion is not available for this claim.')
        return null
      }
      if (!storage.trim()) {
        setError('A larger target storage size is required.')
        return null
      }
      request.storage = storage.trim()
    } else {
      if (info?.snapshot.state !== 'available') {
        setError(info?.snapshot.message || 'The VolumeSnapshot API is not available.')
        return null
      }
      if (!snapshotName.trim()) {
        setError('A snapshot name is required.')
        return null
      }
      if (!snapshotClassName) {
        setError('Choose a VolumeSnapshotClass before reviewing the snapshot.')
        return null
      }
      request.snapshotName = snapshotName.trim()
      request.snapshotClassName = snapshotClassName
    }
    return request
  }

  /** Runs Kubernetes server-side dry-run validation used as the PVC review gate. */
  async function reviewPVCAction() {
    if (!writeEnabled) {
      setError('Enable write mode before reviewing a PVC operation.')
      return
    }
    const request = buildPVCRequest(true, false)
    if (!request) return
    setBusy('review')
    setError('')
    setSuccess('')
    try {
      const result = await applyPVCAction(context, namespace, name, request)
      setReview(result)
      setConfirmed(false)
    } catch (reviewError) {
      setError((reviewError as Error).message)
    } finally {
      setBusy('')
    }
  }

  /** Applies only the exact PVC operation and resource version that passed review. */
  async function applyReviewedPVCAction() {
    if (!writeEnabled) {
      setError('Enable write mode before applying a PVC operation.')
      return
    }
    if (!review) {
      setError('Run the server-side dry-run before applying this PVC operation.')
      return
    }
    if (!confirmed) {
      setError('Confirm that you reviewed the dry-run before applying this PVC operation.')
      return
    }
    const request = buildPVCRequest(false, true)
    if (!request) return
    setBusy('apply')
    setError('')
    setSuccess('')
    try {
      const result = await applyPVCAction(context, namespace, name, request)
      setSuccess(action === 'resize' ? 'PVC resize applied. Kubernetes will complete expansion asynchronously.' : `VolumeSnapshot ${result.snapshotName || snapshotName.trim()} creation requested.`)
      setReview(null)
      setConfirmed(false)
      try {
        await onRefresh()
      } catch (refreshError) {
        setError(`PVC operation applied, but the PVC refresh failed: ${(refreshError as Error).message}`)
      }
    } catch (applyError) {
      setError((applyError as Error).message)
    } finally {
      setBusy('')
    }
  }

  const resizeAvailable = info?.resize.state === 'available'
  const snapshotAvailable = info?.snapshot.state === 'available'
  const selectedAvailable = action === 'resize' ? resizeAvailable : snapshotAvailable
  const disabled = !writeEnabled || Boolean(busy) || Boolean(review) || loading || !selectedAvailable
  const capabilityMessage = action === 'resize' ? info?.resize.message : info?.snapshot.message
  const snapshotClasses = info?.snapshot.classes || []

  return <div className="pvc-operation-panel">
    <div className="pvc-operation-header"><div className="logs-title"><HardDrive size={16} /><strong>PVC operations</strong><span>native storage APIs</span></div><span className="pvc-operation-scope">review then apply</span></div>
    <div className="pvc-operation-intro"><strong>Resize this claim or create a point-in-time VolumeSnapshot.</strong><span>Resize changes only the PVC storage request. Snapshot creation references this PVC and never reads Secret data. Both workflows preserve the current resource version and require a Kubernetes server-side dry-run before confirmation.</span></div>
    {info && <div className="pvc-capability-grid"><div className="pvc-capability-card"><div><strong>Resize</strong><span className={`pvc-state-chip pvc-state-${pvcOperationStateTone(info.resize.state)}`}>{pvcOperationStateLabel(info.resize.state)}</span></div><span>{info.resize.message || 'StorageClass capability is being checked.'}</span>{info.resize.storageClassName && <code>class / {info.resize.storageClassName}</code>}</div><div className="pvc-capability-card"><div><strong>Snapshots</strong><span className={`pvc-state-chip pvc-state-${pvcOperationStateTone(info.snapshot.state)}`}>{pvcOperationStateLabel(info.snapshot.state)}</span></div><span>{info.snapshot.message || 'Snapshot API capability is being checked.'}</span><code>{info.snapshot.classes.length} readable class{info.snapshot.classes.length === 1 ? '' : 'es'}</code></div></div>}
    {!writeEnabled && <div className="operation-lock"><LockKeyhole size={14} /> Enable write mode to review and apply PVC operations. Kubernetes RBAC still authorizes every final request.</div>}
    {loading && <div className="pvc-capability-loading"><LoaderCircle size={15} className="spin" /> Reading StorageClass and VolumeSnapshot capabilities</div>}
    {!loading && info && !selectedAvailable && <div className="pvc-operation-unavailable"><AlertTriangle size={15} /><span>{capabilityMessage || 'This native PVC operation is unavailable for the selected cluster.'}</span></div>}
    <form className="pvc-operation-form" onSubmit={(event) => { event.preventDefault(); void reviewPVCAction() }}>
      <div className="pvc-action-picker"><label className="pvc-operation-field"><span>Operation</span><select value={action} onChange={(event) => changeAction(event.target.value as PVCAction)} disabled={Boolean(busy) || Boolean(review) || loading}><option value="resize">Resize claim</option><option value="snapshot">Create VolumeSnapshot</option></select></label><div className="pvc-action-detail">{action === 'resize' ? 'Increase the requested storage only when the StorageClass explicitly allows expansion.' : 'Create a namespaced VolumeSnapshot from this PVC through snapshot.storage.k8s.io/v1.'}</div></div>
      {action === 'resize' && <div className="pvc-operation-grid"><label className="pvc-operation-field"><span>Current request</span><input value={info?.requestedStorage || storage || '—'} readOnly /></label><label className="pvc-operation-field"><span>Target storage <em>must be larger</em></span><input value={storage} onChange={(event) => setStorage(event.target.value)} disabled={disabled} placeholder="20Gi" /></label></div>}
      {action === 'snapshot' && <div className="pvc-operation-grid"><label className="pvc-operation-field"><span>Snapshot name</span><input value={snapshotName} onChange={(event) => setSnapshotName(event.target.value)} disabled={disabled} placeholder="data-copy" required /></label><label className="pvc-operation-field"><span>VolumeSnapshotClass</span><select value={snapshotClassName} onChange={(event) => setSnapshotClassName(event.target.value)} disabled={disabled}><option value="">Choose a class</option>{snapshotClasses.map((item) => <option key={item.name} value={item.name}>{item.name}{item.driver ? ` / ${item.driver}` : ''}</option>)}</select></label></div>}
      <div className="pvc-operation-actions"><span>{selectedAvailable ? 'Review first; no cluster state changes during dry-run.' : 'The selected native capability must be available before review.'}</span><button type="submit" className="primary-button" disabled={!writeEnabled || Boolean(busy) || Boolean(review) || loading || !selectedAvailable}>{busy === 'review' ? <LoaderCircle size={15} className="spin" /> : <ShieldCheck size={15} />} {busy === 'review' ? 'Reviewing' : 'Review operation'}</button></div>
    </form>
    {review && <div className="pvc-review"><div className="pvc-review-heading"><span className="review-ready"><Check size={13} /> Dry-run passed</span><strong>{action === 'resize' ? 'Resize is ready' : 'Snapshot is ready'}</strong></div><p>Kubernetes accepted this exact resource-versioned request during server-side dry-run. No cluster state has changed; the final request is separate and must be explicitly authorized.</p><label className="pvc-confirm"><input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} disabled={busy === 'apply'} /><span>I reviewed the exact dry-run and explicitly authorize this {action === 'resize' ? 'PVC resize' : 'VolumeSnapshot creation'}.</span></label><div className="pvc-review-actions"><button type="button" className="secondary-button" onClick={() => { setReview(null); setConfirmed(false); setError('') }} disabled={Boolean(busy)}>Change operation</button><button type="button" className="primary-button" onClick={() => void applyReviewedPVCAction()} disabled={busy === 'apply' || !confirmed}>{busy === 'apply' ? <LoaderCircle size={15} className="spin" /> : <Check size={15} />} {busy === 'apply' ? 'Applying' : 'Confirm and apply'}</button></div></div>}
    {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
    {success && <div className="operation-success"><Check size={15} />{success}</div>}
    <div className="pvc-operation-footer"><span><ShieldCheck size={14} /> Full-object PVC update / native snapshot create</span><span>RBAC, conflicts, unsupported APIs, and absent snapshot CRDs stay visible.</span></div>
  </div>
}

/** Describes one native Node operation exposed by the detail form. */
type NodeActionOption = {
  action: NodeAction
  label: string
  detail: string
}

/** Renders a two-step Node operation form with drain impact review and strong confirmation. */
function NodeOperationsPanel({ context, name, object, writeEnabled, onRefresh }: { context: string; name: string; object: ResourceObject; writeEnabled: boolean; onRefresh: () => Promise<void> }) {
  const options = nodeActionOptions()
  const taints = nodeTaints(object)
  const [action, setAction] = useState<NodeAction>('cordon')
  const [taintKey, setTaintKey] = useState('')
  const [taintValue, setTaintValue] = useState('')
  const [taintEffect, setTaintEffect] = useState('NoSchedule')
  const [taintIndex, setTaintIndex] = useState(0)
  const [deleteEmptyDirData, setDeleteEmptyDirData] = useState(false)
  const [ignoreDaemonSets, setIgnoreDaemonSets] = useState(false)
  const [gracePeriod, setGracePeriod] = useState('')
  const [review, setReview] = useState<NodeActionResponse | null>(null)
  const [confirmed, setConfirmed] = useState(false)
  const [confirmationText, setConfirmationText] = useState('')
  const [busy, setBusy] = useState<'review' | 'apply' | ''>('')
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const selectedOption = options.find((option) => option.action === action) || options[0]
  const selectedTaint = taints[taintIndex]
  const nodeIsCordoned = nodeUnschedulable(object)

  useEffect(() => {
    setTaintIndex((current) => Math.min(current, Math.max(taints.length - 1, 0)))
    setReview(null)
    setConfirmed(false)
    setConfirmationText('')
  }, [object])

  /** Clears a review so the operator can change the Node operation. */
  function editNodeAction() {
    setReview(null)
    setConfirmed(false)
    setConfirmationText('')
    setError('')
  }

  /** Changes the selected Node operation and invalidates an older dry-run review. */
  function changeNodeAction(value: NodeAction) {
    setAction(value)
    setReview(null)
    setConfirmed(false)
    setConfirmationText('')
    setError('')
    setSuccess('')
    if (value === 'remove-taint' && taints.length) {
      const next = taints[Math.min(taintIndex, taints.length - 1)]
      setTaintKey(next.key)
      setTaintValue(next.value || '')
      setTaintEffect(next.effect)
    }
  }

  /** Builds and validates the explicit payload sent to the Node action endpoint. */
  function buildNodeRequest(dryRun: boolean): NodeActionRequest | null {
    const request: NodeActionRequest = {
      action,
      dryRun,
      expectedResourceVersion: metadataString(object, 'resourceVersion'),
    }
    if (action === 'add-taint') {
      if (!taintKey.trim()) {
        setError('A taint key is required.')
        return null
      }
      request.taintKey = taintKey.trim()
      request.taintValue = taintValue.trim()
      request.taintEffect = taintEffect
    }
    if (action === 'remove-taint') {
      if (!selectedTaint) {
        setError('This Node has no taints available to remove.')
        return null
      }
      request.taintKey = selectedTaint.key
      request.taintValue = selectedTaint.value || ''
      request.taintEffect = selectedTaint.effect
    }
    if (action === 'drain') {
      request.deleteEmptyDirData = deleteEmptyDirData
      request.ignoreDaemonSets = ignoreDaemonSets
      if (gracePeriod.trim()) {
        const parsedGrace = Number(gracePeriod)
        if (!Number.isInteger(parsedGrace) || parsedGrace < 0 || parsedGrace > 86400) {
          setError('Eviction grace must be a whole number from 0 to 86400 seconds, or blank for each Pod default.')
          return null
        }
        request.gracePeriodSeconds = parsedGrace
      }
    }
    return request
  }

  /** Runs Kubernetes server-side dry-run validation used as the Node review gate. */
  async function reviewNodeAction() {
    if (!writeEnabled) {
      setError('Enable write mode before reviewing a Node operation.')
      return
    }
    const request = buildNodeRequest(true)
    if (!request) return
    setBusy('review')
    setError('')
    setSuccess('')
    try {
      const result = await applyNodeAction(context, name, request)
      setReview(result)
      setConfirmed(false)
      setConfirmationText('')
    } catch (reviewError) {
      setError((reviewError as Error).message)
    } finally {
      setBusy('')
    }
  }

  /** Applies only the exact Node operation and resource version that passed review. */
  async function applyReviewedNodeAction() {
    if (!writeEnabled) {
      setError('Enable write mode before applying a Node operation.')
      return
    }
    if (!review || !review.ready || review.status !== 'dry-run passed') {
      setError('Resolve the drain limitations and pass a server-side dry-run before applying this operation.')
      return
    }
    if (!confirmed) {
      setError('Confirm that you reviewed the Node impact before applying this operation.')
      return
    }
    if (action === 'drain' && confirmationText.trim() !== name) {
      setError(`Type the exact Node name, ${name}, to confirm this drain.`)
      return
    }
    const request = buildNodeRequest(false)
    if (!request) return
    setBusy('apply')
    setError('')
    setSuccess('')
    try {
      const result = await applyNodeAction(context, name, request)
      const drainSummary = action === 'drain' ? ` ${result.evictedCount || 0} Pod(s) evicted; ${result.skippedCount || 0} left in place.` : ''
      setSuccess(`${nodeActionLabel(action)} applied.${drainSummary}`)
      setReview(null)
      setConfirmed(false)
      setConfirmationText('')
      try {
        await onRefresh()
      } catch (refreshError) {
        setError(`Node operation applied, but the Node refresh failed: ${(refreshError as Error).message}`)
      }
    } catch (applyError) {
      setError((applyError as Error).message)
    } finally {
      setBusy('')
    }
  }

  const disabled = !writeEnabled || Boolean(busy) || Boolean(review)
  const canApply = Boolean(review?.ready && review.status === 'dry-run passed' && confirmed && (action !== 'drain' || confirmationText.trim() === name))
  return <div className="node-operation-panel">
    <div className="node-operation-header"><div className="logs-title"><Server size={16} /><strong>Node operations</strong><span>typed core/v1 + policy/v1</span></div><span className="node-operation-scope">review then apply</span></div>
    <div className="node-operation-intro"><strong>{nodeIsCordoned ? 'This Node is currently cordoned.' : 'This Node accepts scheduled workloads.'}</strong><span>Cordon and taint updates use full-object resource-versioned Node updates. Drain is destructive: it can make workloads unavailable, uses Kubernetes eviction APIs, and never force-deletes Pods.</span></div>
    {!writeEnabled && <div className="operation-lock"><LockKeyhole size={14} /> Enable write mode to review and apply Node operations. Kubernetes RBAC still authorizes every final request.</div>}
    <form className="node-operation-form" onSubmit={(event) => { event.preventDefault(); void reviewNodeAction() }}>
      <div className="node-action-picker"><label className="node-operation-field"><span>Operation</span><select value={action} onChange={(event) => changeNodeAction(event.target.value as NodeAction)} disabled={disabled}>{options.map((option) => <option key={option.action} value={option.action}>{option.label}</option>)}</select></label><div className="node-action-detail">{selectedOption?.detail}</div></div>
      {action === 'add-taint' && <div className="node-operation-grid"><label className="node-operation-field"><span>Key</span><input value={taintKey} onChange={(event) => setTaintKey(event.target.value)} disabled={disabled} placeholder="example.com/pool" /></label><label className="node-operation-field"><span>Value <em>optional</em></span><input value={taintValue} onChange={(event) => setTaintValue(event.target.value)} disabled={disabled} placeholder="batch" /></label><label className="node-operation-field"><span>Effect</span><select value={taintEffect} onChange={(event) => setTaintEffect(event.target.value)} disabled={disabled}><option value="NoSchedule">NoSchedule</option><option value="PreferNoSchedule">PreferNoSchedule</option><option value="NoExecute">NoExecute</option></select></label></div>}
      {action === 'remove-taint' && <div className="node-operation-grid"><label className="node-operation-field node-operation-field-wide"><span>Existing taint</span>{taints.length ? <select value={taintIndex} onChange={(event) => setTaintIndex(Number(event.target.value))} disabled={disabled}>{taints.map((taint, index) => <option key={`${taint.key}-${taint.effect}-${index}`} value={index}>{nodeTaintLabel(taint)}</option>)}</select> : <div className="node-empty-field">No taints are present on this Node.</div>}</label></div>}
      {action === 'drain' && <div className="node-drain-options"><label className="node-operation-field"><span>Eviction grace <em>blank = each Pod default</em></span><input type="number" min="0" max="86400" step="1" value={gracePeriod} onChange={(event) => setGracePeriod(event.target.value)} disabled={disabled} placeholder="Pod default" /></label><label className="node-check-option"><input type="checkbox" checked={ignoreDaemonSets} onChange={(event) => setIgnoreDaemonSets(event.target.checked)} disabled={disabled} /><span><strong>Leave DaemonSet Pods in place</strong><small>Required for Nodes running DaemonSet agents; those Pods will remain.</small></span></label><label className="node-check-option"><input type="checkbox" checked={deleteEmptyDirData} onChange={(event) => setDeleteEmptyDirData(event.target.checked)} disabled={disabled} /><span><strong>Allow emptyDir data loss</strong><small>Required to evict Pods using local emptyDir volumes. This is a destructive acknowledgement.</small></span></label></div>}
      <div className="node-operation-actions"><span>{selectedOption?.detail}</span><button type="submit" className="primary-button" disabled={!writeEnabled || Boolean(busy) || Boolean(review)}>{busy === 'review' ? <LoaderCircle size={15} className="spin" /> : <ShieldCheck size={15} />} {busy === 'review' ? 'Reviewing' : 'Review operation'}</button></div>
    </form>
    {review && <div className={`node-review ${review.ready ? '' : 'node-review-blocked'}`}><div className="node-review-heading"><span className={review.ready ? 'review-ready' : 'node-review-blocked-chip'}>{review.ready ? <><Check size={13} /> Dry-run passed</> : <><AlertTriangle size={13} /> Review blocked</>}</span><strong>{nodeActionLabel(action)} {review.ready ? 'is ready' : 'needs attention'}</strong></div><p>{review.ready ? 'Kubernetes accepted this exact resource-versioned request during server-side dry-run. No cluster state has changed.' : 'No mutation was submitted. Resolve or explicitly acknowledge every listed Pod limitation, then refresh and review again.'}</p>{review.limitations?.length ? <div className="node-limitation-list"><strong>Safety notes</strong>{review.limitations.map((limitation) => <span key={limitation}><AlertTriangle size={12} />{limitation}</span>)}</div> : null}{review.pods?.length ? <div className="node-impact-table"><div className="node-impact-heading"><span>Pod impact</span><span>{review.podCount || review.pods.length} scheduled</span></div>{review.pods.map((pod) => <div className="node-impact-row" key={`${pod.namespace}/${pod.name}`}><div><strong>{pod.namespace}/{pod.name}</strong><span>{pod.phase}{pod.controller ? ` / ${pod.controller}` : ' / unmanaged'}</span></div><span className={`node-impact-status node-impact-${pod.action}`}>{pod.action}{pod.usesEmptyDir ? ' / emptyDir' : ''}</span></div>)}</div> : null}{review.ready && <><label className="node-confirm"><input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} disabled={busy === 'apply'} /><span>I reviewed the Kubernetes dry-run and understand this operation changes Node scheduling state.</span></label>{action === 'drain' && <label className="node-operation-field node-confirm-name"><span>Type Node name to confirm <em>{name}</em></span><input value={confirmationText} onChange={(event) => setConfirmationText(event.target.value)} disabled={busy === 'apply'} placeholder={name} /></label>}<div className="node-review-actions"><button type="button" className="secondary-button" onClick={editNodeAction} disabled={Boolean(busy)}>Change operation</button><button type="button" className="primary-button" onClick={() => void applyReviewedNodeAction()} disabled={busy === 'apply' || !canApply}>{busy === 'apply' ? <LoaderCircle size={15} className="spin" /> : <Check size={15} />} {busy === 'apply' ? 'Applying' : action === 'drain' ? 'Confirm and drain' : 'Confirm and apply'}</button></div></>}{error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}{success && <div className="operation-success"><Check size={15} />{success}</div>}</div>}
    <div className="node-operation-footer"><span><ShieldCheck size={14} /> Native Node update / eviction</span><span>RBAC, conflicts, unsupported APIs, and Pod limits stay visible.</span></div>
  </div>
}

/** Returns the native Node operation choices shown in the detail form. */
function nodeActionOptions(): NodeActionOption[] {
  return [
    { action: 'cordon', label: 'Cordon Node', detail: 'Mark the Node unschedulable while existing Pods continue running.' },
    { action: 'uncordon', label: 'Uncordon Node', detail: 'Allow the scheduler to place new Pods on this Node again.' },
    { action: 'drain', label: 'Drain Node', detail: 'Destructive: cordon first, then evict eligible managed Pods through policy/v1 without force deletion.' },
    { action: 'add-taint', label: 'Add taint', detail: 'Add one validated key/effect/value scheduling rule without replacing another taint.' },
    { action: 'remove-taint', label: 'Remove taint', detail: 'Remove one exact existing key/effect/value tuple after a fresh Node read.' },
  ]
}

/** Returns true only for the built-in cluster-scoped Node resource. */
function targetedNodeResource(definition: ResourceDefinition): boolean {
  return definition.group === '' && definition.version === 'v1' && definition.resource === 'nodes' && !definition.namespaced
}

/** Returns true only for the namespaced built-in PersistentVolumeClaim resource. */
function targetedPVCResource(definition: ResourceDefinition): boolean {
  return definition.group === '' && definition.version === 'v1' && definition.resource === 'persistentvolumeclaims' && definition.namespaced
}

/** Converts one PVC capability state into a compact panel label. */
function pvcOperationStateLabel(state: PVCOperationState): string {
  switch (state) {
    case 'available':
      return 'Available'
    case 'permission-denied':
      return 'RBAC denied'
    case 'unsupported':
      return 'Unsupported'
    case 'unavailable':
      return 'Unavailable'
    case 'empty':
      return 'No classes'
    default:
      return 'Read failed'
  }
}

/** Maps a PVC capability state to the existing field-console color language. */
function pvcOperationStateTone(state: PVCOperationState): 'good' | 'warning' | 'neutral' {
  if (state === 'available') return 'good'
  if (state === 'permission-denied' || state === 'error') return 'warning'
  return 'neutral'
}

/** Returns the user-facing name for one native Node action. */
function nodeActionLabel(action: NodeAction): string {
  const labels: Record<NodeAction, string> = {
    cordon: 'Cordon',
    uncordon: 'Uncordon',
    drain: 'Drain',
    'add-taint': 'Add taint',
    'remove-taint': 'Remove taint',
  }
  return labels[action]
}

/** Reads the current Node unschedulable flag from an unstructured resource. */
function nodeUnschedulable(object: ResourceObject): boolean {
  return nestedObjectValue(object, ['spec', 'unschedulable']) === true
}

/** Reads validated-looking taint summaries from an unstructured Node object for form display. */
function nodeTaints(object: ResourceObject): NodeTaint[] {
  const rawTaints = nestedObjectValue(object, ['spec', 'taints'])
  if (!Array.isArray(rawTaints)) return []
  return rawTaints.flatMap((entry) => {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return []
    const candidate = entry as Record<string, unknown>
    if (typeof candidate.key !== 'string' || typeof candidate.effect !== 'string') return []
    return [{ key: candidate.key, value: typeof candidate.value === 'string' ? candidate.value : '', effect: candidate.effect }]
  })
}

/** Formats one exact Node taint tuple for review and removal selection. */
function nodeTaintLabel(taint: NodeTaint): string {
  return `${taint.key}${taint.value ? `=${taint.value}` : ''}:${taint.effect}`
}

/** Returns the supported targeted action list for one native workload resource. */
function workloadActionOptions(resource: string): WorkloadActionOption[] {
  const options: Record<string, WorkloadActionOption[]> = {
    deployments: [
      { action: 'scale', label: 'Scale replicas', detail: 'Change the desired Deployment replica count through the native scale subresource.' },
      { action: 'restart', label: 'Restart rollout', detail: 'Bump the standard pod-template restart annotation to start a new rollout.' },
      { action: 'pause', label: 'Pause rollout', detail: 'Pause Deployment progression while preserving its desired state.' },
      { action: 'resume', label: 'Resume rollout', detail: 'Resume a Deployment that is currently paused.' },
      { action: 'rollback', label: 'Rollback revision', detail: 'Restore a Deployment pod template from an owned ReplicaSet revision.' },
      { action: 'image', label: 'Change image', detail: 'Replace one regular container image in the Deployment pod template.' },
    ],
    statefulsets: [
      { action: 'scale', label: 'Scale replicas', detail: 'Change the desired StatefulSet replica count through the native scale subresource.' },
      { action: 'restart', label: 'Restart rollout', detail: 'Bump the standard pod-template restart annotation to replace StatefulSet Pods.' },
      { action: 'image', label: 'Change image', detail: 'Replace one regular container image in the StatefulSet pod template.' },
    ],
    daemonsets: [
      { action: 'restart', label: 'Restart rollout', detail: 'Bump the standard pod-template restart annotation to replace DaemonSet Pods.' },
      { action: 'image', label: 'Change image', detail: 'Replace one regular container image in the DaemonSet pod template.' },
    ],
    jobs: [
      { action: 'run-now', label: 'Run again now', detail: 'Create a fresh one-off Job from this Job template without reusing controller identity.' },
    ],
    cronjobs: [
      { action: 'run-now', label: 'Run now', detail: 'Create a one-off Job from the current CronJob template immediately.' },
      { action: 'pause', label: 'Pause schedule', detail: 'Suspend new CronJob schedules while existing Jobs continue independently.' },
      { action: 'resume', label: 'Resume schedule', detail: 'Allow the CronJob controller to create Jobs on schedule again.' },
      { action: 'schedule', label: 'Change schedule', detail: 'Update the CronJob schedule expression after Kubernetes validates it.' },
      { action: 'image', label: 'Change image', detail: 'Replace one regular container image in future CronJob Jobs.' },
    ],
  }
  return options[resource] || []
}

/** Returns true only for the built-in API resources with targeted workload forms. */
function targetedWorkloadResource(definition: ResourceDefinition): boolean {
  return definition.version === 'v1' && ((definition.group === 'apps' && ['deployments', 'statefulsets', 'daemonsets'].includes(definition.resource)) || (definition.group === 'batch' && ['jobs', 'cronjobs'].includes(definition.resource)))
}

/** Returns the user-facing name for one targeted workload action. */
function workloadActionLabel(action: WorkloadAction): string {
  const labels: Record<WorkloadAction, string> = {
    scale: 'Scale',
    restart: 'Restart',
    pause: 'Pause',
    resume: 'Resume',
    rollback: 'Rollback',
    image: 'Image change',
    schedule: 'Schedule change',
    'run-now': 'Run now',
  }
  return labels[action]
}

/** Returns regular container names from the selected workload pod template. */
function workloadContainerNames(resource: string, object: ResourceObject): string[] {
  const containers = nestedWorkloadValue(object, resource, ['containers'])
  if (!Array.isArray(containers)) return []
  return containers.flatMap((entry) => {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return []
    const name = (entry as Record<string, unknown>).name
    return typeof name === 'string' && name ? [name] : []
  })
}

/** Returns the selected regular container image from a workload pod template. */
function workloadContainerImage(resource: string, object: ResourceObject, containerName: string): string {
  const containers = nestedWorkloadValue(object, resource, ['containers'])
  if (!Array.isArray(containers)) return ''
  for (const entry of containers) {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) continue
    const candidate = entry as Record<string, unknown>
    if (candidate.name === containerName && typeof candidate.image === 'string') return candidate.image
  }
  return ''
}

/** Returns the current desired replica count for a scalable workload. */
function workloadReplicaCount(object: ResourceObject): number {
  const replicas = nestedObjectValue(object, ['spec', 'replicas'])
  return typeof replicas === 'number' && Number.isFinite(replicas) ? replicas : 1
}

/** Returns the current CronJob schedule expression. */
function workloadSchedule(object: ResourceObject): string {
  const schedule = nestedObjectValue(object, ['spec', 'schedule'])
  return typeof schedule === 'string' ? schedule : ''
}

/** Reads a value from a workload template path while keeping generic objects safe. */
function nestedWorkloadValue(object: ResourceObject, resource: string, suffix: string[]): unknown {
  const path = resource === 'cronjobs' ? ['spec', 'jobTemplate', 'spec', 'template', 'spec', ...suffix] : ['spec', 'template', 'spec', ...suffix]
  let value: unknown = object
  for (const field of path) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
    value = (value as Record<string, unknown>)[field]
  }
  return value
}

/** Reads a direct field path from an unstructured resource object. */
function nestedObjectValue(object: ResourceObject, path: string[]): unknown {
  let value: unknown = object
  for (const field of path) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
    value = (value as Record<string, unknown>)[field]
  }
  return value
}

/** Reads the current requested storage string from a generic PVC object. */
function pvcRequestedStorage(object: ResourceObject): string {
  const value = nestedObjectValue(object, ['spec', 'resources', 'requests', 'storage'])
  return typeof value === 'string' ? value : ''
}

/** Renders one related-resource target as a deep-link when it is verified and discovered. */
function RelatedResourceRow({ item, onOpenResource }: { item: RelatedResource; onOpenResource: (definition: ResourceDefinition, name: string, namespace?: string) => void }) {
  const canOpen = item.state === 'resolved' && Boolean(item.definition && item.name)
  const kind = item.kind || item.definition?.kind || 'Resource'
  const name = item.name || 'Unnamed target'
  const row = <>
    <span className="relationship-icon"><Network size={16} /></span>
    <div className="relationship-main">
      <div className="relationship-labels"><span className="relationship-kind">{relatedTypeLabel(item.type)}</span>{item.controller && <span className="relationship-flag">Controller</span>}{item.blockOwnerDeletion && <span className="relationship-flag">Deletion blocked</span>}</div>
      <strong>{kind}/{name}</strong>
      <span className="relationship-meta"><code>{item.source}</code>{item.namespace ? <><span>·</span><code>ns {item.namespace}</code></> : null}{item.uid ? <><span>·</span><code>uid {item.uid.slice(0, 12)}</code></> : null}</span>
    </div>
    {item.state === 'resolved' ? <span className="relationship-open">Open <ArrowUpRight size={13} /></span> : <span className={`relationship-unresolved relationship-state-${item.state}`}>{relatedStateLabel(item.state)}{item.message ? ` / ${item.message}` : ''}</span>}
  </>
  if (!canOpen || !item.definition || !item.name) return <div className={`relationship-row relationship-row-state-${item.state}`}>{row}</div>
  const namespace = item.definition.namespaced ? item.namespace : undefined
  return <button type="button" className="relationship-row relationship-row-button" aria-label={`Open related ${kind}/${name}`} onClick={() => onOpenResource(item.definition!, item.name!, namespace)}>{row}</button>
}

/** Renders one explicit partial, permission, malformed, or unavailable relationship source. */
function RelatedNotice({ notice }: { notice: { type: string; source: string; state: string; message: string } }) {
  return <div className={`relationship-notice relationship-notice-${notice.state}`}><span className="relationship-notice-mark"><AlertTriangle size={14} /></span><div><div className="relationship-notice-heading"><strong>{relatedTypeLabel(notice.type)}</strong><span className={`relationship-state-chip relationship-state-${notice.state}`}>{relatedStateLabel(notice.state)}</span></div><code>{notice.source}</code><span>{notice.message}</span></div></div>
}

/** Converts an aggregate relationship state into a compact panel action label. */
function relatedAggregateLabel(state: RelatedAggregateState, count: number): string {
  if (state === 'permission-denied') return 'RBAC denied'
  if (state === 'error') return 'Read error'
  if (state === 'partial') return `${count} link${count === 1 ? '' : 's'} / partial`
  if (state === 'unavailable') return 'Sources limited'
  if (state === 'empty') return 'No links'
  return `${count} link${count === 1 ? '' : 's'}`
}

/** Converts an aggregate relationship state into the field-console panel tone. */
function relatedAggregateTone(state: RelatedAggregateState): 'good' | 'warning' | 'neutral' {
  if (state === 'available') return 'good'
  if (state === 'empty' || state === 'unavailable') return 'neutral'
  return 'warning'
}

/** Converts a relationship type into readable field-console copy. */
function relatedTypeLabel(type: string): string {
  const labels: Record<string, string> = {
    owner: 'Owner reference',
    'owned-descendant': 'Owned descendant',
    'selector-match': 'Selector match',
    'selected-by': 'Selected by Service',
    'bound-volume': 'Bound volume',
    'bound-claim': 'Bound claim',
    reference: 'Native reference',
    aggregation: 'Relationship scan',
    discovery: 'API discovery',
  }
  return labels[type] || titleCase(type)
}

/** Converts a relationship target state into a short explicit status label. */
function relatedStateLabel(state: string): string {
  if (state === 'permission-denied') return 'RBAC denied'
  if (state === 'unresolved') return 'Unresolved'
  if (state === 'unavailable') return 'Unavailable'
  if (state === 'partial') return 'Partial'
  if (state === 'error') return 'Read error'
  return state
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
  const selectedContextAvailable = contexts.some((context) => context.name === value)
  return <label className="select-shell context-select"><span className="select-icon"><Server size={15} /></span><select value={value} onChange={(event) => onChange(event.target.value)} disabled={!contexts.length}><option value="">No context</option>{value && !selectedContextAvailable && <option value={value}>{value} (unavailable)</option>}{contexts.map((context) => <option key={context.name} value={context.name}>{context.name}</option>)}</select><ChevronDown size={14} /></label>
}

/** Renders and persists the active namespace selector. */
function NamespacePicker({ context, value, revision, onChange }: { context: string; value: string; revision: number; onChange: (value: string) => void }) {
  const [namespaces, setNamespaces] = useState<string[]>([])
  useEffect(() => {
    if (!context) return
    const definition = commonDefinitions.find((item) => item.resource === 'namespaces')!
    fetchResources(context, definition, 'all').then((response) => {
      const nextNamespaces = response.items.map((item) => item.name).sort()
      setNamespaces(nextNamespaces)
      if (value !== 'all' && !nextNamespaces.includes(value)) onChange('all')
    }).catch(() => setNamespaces([]))
  }, [context, revision])
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

/** Resolves a resource slug against discovered definitions and common fallbacks. */
function resolveDefinition(resource: string, definitions: ResourceDefinition[]): ResourceDefinition {
  return definitions.find((item) => item.resource === resource) ?? commonDefinitions.find((item) => item.resource === resource) ?? { group: '', version: 'v1', resource, kind: titleCase(resource), namespaced: true, verbs: [] }
}

/** Identifies custom-resource candidates without probing built-in API groups for CRD schemas. */
function isCustomResourceDefinition(definition: ResourceDefinition): boolean {
  if (definition.customResource) return true
  if (!definition.group) return false
  return !['admissionregistration.k8s.io', 'apiextensions.k8s.io', 'authentication.k8s.io', 'authorization.k8s.io', 'autoscaling', 'batch', 'certificates.k8s.io', 'coordination.k8s.io', 'discovery.k8s.io', 'events.k8s.io', 'extensions', 'flowcontrol.apiserver.k8s.io', 'gateway.networking.k8s.io', 'metrics.k8s.io', 'networking.k8s.io', 'node.k8s.io', 'policy', 'rbac.authorization.k8s.io', 'resource.k8s.io', 'scheduling.k8s.io', 'snapshot.storage.k8s.io', 'storage.k8s.io'].includes(definition.group)
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

/** Produces a stable namespace/name key for selection and result reconciliation. */
function resourceItemKey(item: { name: string; namespace?: string }): string {
  return `${item.namespace || ''}/${item.name}`
}

/** Converts a resource summary into the bounded coordinates sent for deletion review. */
function resourceDeleteItem(item: ResourceSummary): ResourceDeleteItem {
  return { name: item.name, namespace: item.namespace, resourceVersion: item.resourceVersion }
}

/** Captures the reviewed namespace/name and resource version for stale-review detection. */
function resourceDeleteReviewKey(item: ResourceSummary): string {
  return `${resourceItemKey(item)}@${item.resourceVersion}`
}

/** Builds the exact bounded phrase required before a bulk delete mutation. */
function bulkDeleteConfirmation(count: number): string {
  return `DELETE ${count}`
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
