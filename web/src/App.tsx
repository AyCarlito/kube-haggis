import {
  Activity,
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  ArrowUpRight,
  Bookmark,
  BookmarkPlus,
  Box,
  Bug,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  CircleDot,
  Clock3,
  Command,
  Database,
  Download,
  FileCode2,
  Gauge,
  HardDrive,
  History,
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
  fetchAudit,
  fetchContexts,
  fetchDiagnostics,
  fetchGlobalSearch,
  fetchResourceCatalog,
  fetchOverview,
  fetchPVCOperations,
  fetchResource,
  fetchResourceSchema,
  fetchResourceEvents,
  fetchOperationalReadPack,
  fetchRelated,
  fetchRelatedReverse,
  fetchResources,
  downloadPodFile,
  launchEphemeralContainer,
  logsUrl,
  portForwardUrl,
  uploadPodFile,
  resourceEventsWatchUrl,
  reloadContexts as reloadKubeconfig,
  responseApiError,
  resourceConflictFromError,
  terminalUrl,
  watchUrl,
} from './api'
import {
  catalogEventResourcePath,
  catalogResourcePath,
  catalogStaticNavigationPath,
  filterResourceCatalog,
  groupResourceCatalog,
  isAmbiguousCatalogRoute,
  parseResourceRoute,
  relatedResourceDetailPath,
  resolveCatalogDefinition,
} from './catalog'
import { overviewEventDetailPath, overviewEventsPath, overviewMetricDestination, overviewNavigationTargets, workloadNavigationTargets, type WorkloadNavigationTarget } from './overview'
import { hasSearchFilters, emptySearchQuery, normalizeSearchQuery, parseSearchQuery, searchGroupOptions, searchHref, searchKindOptions, searchQueryEquals, searchResourceOptions, searchResultKey, searchResultResourcePath, searchDefinitionCandidates } from './search'
import { createSavedSearchView, isSavedSearchViewApplicable, loadSavedSearchViews, persistSavedSearchViews, removeSavedSearchView, savedSearchViewState, upsertSavedSearchView, type SavedViewState } from './savedViews'
import { customResourceCapabilityLabel, printerColumnDisplay, schemaFallbackSummary, schemaStatusNotice } from './crd'
import { isOperationalReadPackResource, operationalReadPackStateLabel } from './readPack'
import { parseSchemaInput, projectSchemaIdentityFields, validateSchemaScalarValue } from './schemaForm'
import { ReliableWatchController, initialWatchSnapshot, type FreshnessState, type WatchSnapshot } from './watch'
import { parseWorkloadReplicaInput, workloadReplicaInput, WORKLOAD_REPLICA_VALIDATION_MESSAGE } from './workload'
import { MetricSample } from './metricSample'
import type {
  AuditEntry,
  AuditResponse,
  ClusterOverview,
  ContextSummary,
  ContextsResponse,
  DiagnosticsErrorEvent,
  DiagnosticsResponse,
  DiagnosticsWatchEvent,
  DiagnosticsWatchView,
  CustomResourceCapabilities,
  CustomResourcePrinterColumn,
  EphemeralContainerRequest,
  EventSummary,
  PortForwardMessage,
  ResourceDefinition,
  ResourceCatalogResponse,
  ResourceDeleteItem,
  ResourceDeleteResponse,
  BulkDeleteResponse,
  ResourceConflictResponse,
  ResourceEventsResponse,
  ResourceListResponse,
  ResourceObject,
  ResourceSchemaField,
  ResourceSchemaResponse,
  OperationalReadPackResponse,
  OperationalReadPackPanel as OperationalReadPackPanelModel,
  IngressReadPackData,
  EndpointSliceReadPackData,
  NetworkPolicyReadPackData,
  HorizontalPodAutoscalerReadPackData,
  PodDisruptionBudgetReadPackData,
  ResourceQuotaReadPackData,
  LimitRangeReadPackData,
  PersistentVolumeReadPackData,
  StorageClassReadPackData,
  ServiceAccountReadPackData,
  PodReadPackData,
  OperationalReadPackReference,
  OperationalCondition,
  PodContainerSummary,
  ResourceSummary,
  SavedSearchView,
  SearchQuery,
  SearchResponse,
  SearchResult,
  SearchSource,
  RelatedAggregateState,
  RelatedResourcesResponse,
  RelatedResource,
  RouteState,
  RetryClassification,
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
  { group: 'apps', version: 'v1', resource: 'deployments', kind: 'Deployment', namespaced: true, scope: 'Namespaced', verbs: [] },
  { group: 'apps', version: 'v1', resource: 'statefulsets', kind: 'StatefulSet', namespaced: true, scope: 'Namespaced', verbs: [] },
  { group: 'apps', version: 'v1', resource: 'daemonsets', kind: 'DaemonSet', namespaced: true, scope: 'Namespaced', verbs: [] },
  { group: 'batch', version: 'v1', resource: 'jobs', kind: 'Job', namespaced: true, scope: 'Namespaced', verbs: [] },
  { group: 'batch', version: 'v1', resource: 'cronjobs', kind: 'CronJob', namespaced: true, scope: 'Namespaced', verbs: [] },
  { group: '', version: 'v1', resource: 'pods', kind: 'Pod', namespaced: true, scope: 'Namespaced', verbs: [] },
  { group: '', version: 'v1', resource: 'services', kind: 'Service', namespaced: true, scope: 'Namespaced', verbs: [] },
  { group: '', version: 'v1', resource: 'configmaps', kind: 'ConfigMap', namespaced: true, scope: 'Namespaced', verbs: [] },
  { group: '', version: 'v1', resource: 'secrets', kind: 'Secret', namespaced: true, scope: 'Namespaced', verbs: [] },
  { group: '', version: 'v1', resource: 'persistentvolumeclaims', kind: 'PersistentVolumeClaim', namespaced: true, scope: 'Namespaced', verbs: [] },
  { group: '', version: 'v1', resource: 'nodes', kind: 'Node', namespaced: false, scope: 'Cluster', verbs: [] },
  { group: '', version: 'v1', resource: 'namespaces', kind: 'Namespace', namespaced: false, scope: 'Cluster', verbs: [] },
  { group: '', version: 'v1', resource: 'events', kind: 'Event', namespaced: true, scope: 'Namespaced', verbs: [] },
]

/** Identifies the safe summary columns available in every generic resource table. */
type ResourceColumnKey = 'name' | 'namespace' | 'status' | 'ready' | 'age' | 'labels' | 'resourceVersion'

/** Identifies the generic summary field used for local page sorting. */
type ResourceSortKey = 'name' | 'namespace' | 'status' | 'ready' | 'age'

/** Identifies the direction used by a local resource-page sort. */
type ResourceSortDirection = 'asc' | 'desc'

/** Describes one configurable generic resource table column. */
interface ResourceColumnDefinition {
  key: ResourceColumnKey
  label: string
  required?: boolean
}

/** Tracks the opaque continuation tokens already visited for one list query. */
interface ResourcePaginationState {
  queryKey: string
  pageIndex: number
  tokens: string[]
}

/** Defines the stable generic columns offered by the resource table. */
const resourceColumnDefinitions: ResourceColumnDefinition[] = [
  { key: 'name', label: 'Name', required: true },
  { key: 'namespace', label: 'Namespace' },
  { key: 'status', label: 'Status' },
  { key: 'ready', label: 'Ready' },
  { key: 'age', label: 'Age' },
  { key: 'labels', label: 'Labels' },
  { key: 'resourceVersion', label: 'Resource version' },
]

/** Defines the useful default summary columns for a dense operator table. */
const defaultResourceColumns: ResourceColumnKey[] = ['name', 'namespace', 'status', 'ready', 'age', 'labels']

/** Defines the bounded page sizes sent to Kubernetes for continuation-token pagination. */
const resourcePageSizes = [25, 50, 100]

const navGroups = [
  {
    label: 'Observe',
    items: [
      { label: 'Cluster overview', resource: undefined, icon: Gauge },
      { label: 'Global search', path: '/search', icon: Search },
      { label: 'API resource catalog', path: '/catalog', icon: Box },
      { label: 'Pods', resource: 'pods', icon: CircleDot },
      { label: 'Nodes', resource: 'nodes', icon: Server },
      { label: 'Events', resource: 'events', icon: Activity },
      { label: 'Mutation history', path: '/history', icon: History },
      { label: 'Diagnostics', path: '/diagnostics', icon: Settings2 },
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
      { label: 'Ingress', resource: 'ingresses', icon: Network },
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
  const [definitions, setDefinitions] = useState<ResourceDefinition[]>([])
  const [catalog, setCatalog] = useState<ResourceCatalogResponse | null>(null)
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [catalogError, setCatalogError] = useState('')
  const [catalogRefreshNonce, setCatalogRefreshNonce] = useState(0)
  const [writeEnabled, setWriteEnabled] = useState(false)
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const [contextLoading, setContextLoading] = useState(true)
  const [contextRequestError, setContextRequestError] = useState('')
  const [kubeconfigReloadEpoch, setKubeconfigReloadEpoch] = useState(0)
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
    setCatalog(null)
    setDefinitions([])
    setCatalogLoading(true)
    setCatalogError('')
    fetchResourceCatalog(selectedContext, catalogRefreshNonce > 0)
      .then((nextCatalog) => {
        if (active) {
          setCatalog(nextCatalog)
          setDefinitions(nextCatalog.items)
          setCatalogError('')
        }
      })
      .catch((loadError: Error) => {
        if (active) {
          setCatalog(null)
          setDefinitions([])
          setCatalogError(loadError.message)
        }
      })
      .finally(() => active && setCatalogLoading(false))
    return () => {
      active = false
    }
  }, [selectedContext, contextRevision, catalogRefreshNonce])

  /** Forces a kubeconfig reload while preserving the selected context and namespace. */
  async function reloadKubeconfigSources() {
    const requestID = contextRequestID.current + 1
    contextRequestID.current = requestID
    setKubeconfigReloadEpoch((value) => value + 1)
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

  /** Retries API discovery and invalidates the server-side catalog cache. */
  function retryCatalog() {
    setCatalogRefreshNonce((value) => value + 1)
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

  const activeDefinition = route.resource ? resolveCatalogDefinition(route, definitions) : undefined
  const ambiguousShortRoute = route.resource ? isAmbiguousCatalogRoute(route, definitions) : false
  const activeContext = contexts?.contexts.find((item) => item.name === selectedContext)
  const validResourceName = route.name && route.name !== 'undefined' && route.name !== 'null' ? route.name : undefined
  const content = !contexts || (contexts.contexts.length > 0 && !selectedContext) ? (
    <LoadingState label="Reading local kubeconfig" />
  ) : route.view === 'diagnostics' ? (
    <DiagnosticsPage context={selectedContext} revision={contextRevision} />
  ) : route.view === 'history' ? (
    <MutationHistoryPage contexts={contexts} selectedContext={selectedContext} activeContext={activeContext} revision={contextRevision} />
  ) : route.view === 'search' ? (
    <GlobalSearchPage context={selectedContext} namespace={selectedNamespace} revision={contextRevision} searchKey={route.search || ''} definitions={definitions} catalog={catalog} catalogLoading={catalogLoading} onNamespace={changeNamespace} onOpen={(result) => navigate(searchResultResourcePath(result), result.resource, result.name, result.namespace || undefined)} />
  ) : route.view === 'catalog' ? (
    <ResourceCatalogPage catalog={catalog} loading={catalogLoading} error={catalogError} onRetry={retryCatalog} onOpen={(definition) => navigate(catalogResourcePath(definition), definition.resource)} />
  ) : contexts.error || contexts.contexts.length === 0 ? (
    <SetupState error={contexts.error} />
  ) : route.view === 'workloads' ? (
    <WorkloadsPage context={selectedContext} namespace={selectedNamespace} definitions={definitions} catalogLoading={catalogLoading} catalogError={catalogError} />
  ) : route.resource && catalogLoading && !catalog ? (
    <LoadingState label="Reading the Kubernetes API catalog" />
  ) : route.resource && catalogError && !catalog ? (
    <CatalogState title="Resource catalog unavailable" detail={catalogError} onRetry={retryCatalog} />
  ) : route.resource && ambiguousShortRoute ? (
    <CatalogState title="Resource route is ambiguous" detail={`The short route /resources/${route.resource} matches more than one discovered API resource. Open the catalog and choose its full group/version/resource identity.`} onRetry={() => navigate('/catalog')} />
  ) : route.resource && !activeDefinition ? (
    <CatalogState title="Resource is not in the catalog" detail="This deep link does not match a currently discovered group, version, and resource. Refresh discovery before retrying." onRetry={retryCatalog} />
  ) : validResourceName && activeDefinition ? (
    <ResourceDetailPage
      context={selectedContext}
      namespace={route.namespace ?? selectedNamespace}
      definition={activeDefinition}
      name={validResourceName}
      writeEnabled={writeEnabled}
      revision={contextRevision}
      reloadEpoch={kubeconfigReloadEpoch}
      onBack={() => navigate(catalogResourcePath(activeDefinition), activeDefinition.resource)}
      onOpenEvent={(event) => navigate(catalogEventResourcePath(definitions, event.name), 'events', event.name, event.namespace || undefined)}
      onOpenResource={(definition, resourceName, resourceNamespace) => navigate(catalogResourcePath(definition, resourceName), definition.resource, resourceName, resourceNamespace)}
    />
  ) : route.resource && activeDefinition ? (
    <ResourceListPage
      context={selectedContext}
      namespace={selectedNamespace}
      definition={activeDefinition}
      revision={contextRevision}
      reloadEpoch={kubeconfigReloadEpoch}
      writeEnabled={writeEnabled}
      onOpen={(name, itemNamespace) => navigate(catalogResourcePath(activeDefinition, name), activeDefinition.resource, name, activeDefinition.namespaced && selectedNamespace === 'all' ? itemNamespace : undefined)}
      onCreate={() => navigate(catalogResourcePath(activeDefinition, 'new'), activeDefinition.resource, 'new')}
    />
  ) : (
    <OverviewPage context={selectedContext} namespace={selectedNamespace} definitions={definitions} contexts={contexts} activeContext={activeContext} revision={contextRevision} />
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
                const isActive = 'path' in item ? route.path === item.path : item.resource ? route.resource === item.resource : !route.resource && !route.view
                const destination = 'path' in item && item.path ? item.path : item.resource ? catalogStaticNavigationPath(item.resource, definitions) : '/'
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
            <span>{route.view === 'history' ? 'Mutation history' : route.view === 'diagnostics' ? 'Diagnostics' : route.view === 'search' ? 'Global search' : route.view === 'catalog' ? 'API catalog' : route.view === 'workloads' ? 'Workloads' : route.resource ? activeDefinition?.kind || titleCase(route.resource) : 'Cluster overview'}</span>
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
            <button className="icon-button" onClick={() => navigate('/diagnostics')} aria-label="Open diagnostics" title="Open diagnostics">
              <Settings2 size={18} />
            </button>
          </div>
        </header>
        <div className="page-scroll">
          {contexts && !(contexts.error && contexts.contexts.length === 0) && (contextLoading || contexts.sourceState === 'changed' || Boolean(contexts.reloadError) || Boolean(contextRequestError) || (Boolean(selectedContext) && contexts.contexts.length > 0 && !activeContext)) ? <KubeconfigNotice state={contexts.sourceState} error={contexts.reloadError || contextRequestError} selectedContext={selectedContext} selectedContextAvailable={Boolean(activeContext)} loading={contextLoading} onReload={() => void reloadKubeconfigSources()} /> : null}
          {selectedContext && (catalogLoading || Boolean(catalogError) || (catalog && catalog.state !== 'available')) ? <DiscoveryNotice catalog={catalog} loading={catalogLoading} error={catalogError} onRetry={retryCatalog} /> : null}
          {content}
        </div>
      </main>
    </div>
  )
}

/** Renders the cluster overview and its health summary panels. */
function OverviewPage({ context, namespace, definitions, contexts, activeContext, revision }: { context: string; namespace: string; definitions: ResourceDefinition[]; contexts: ContextsResponse | null; activeContext?: ContextSummary; revision: number }) {
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
  const targets = overviewNavigationTargets(definitions)
  const eventsPath = overviewEventsPath(definitions)
  const eventsHref = eventsPath ? routeHref(eventsPath, context, namespace) : undefined
  const eventScope = namespace === 'all' ? 'all namespaces' : `namespace ${namespace}`

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
        <MetricCard label="Nodes ready" value={`${overview.nodes.ready}/${overview.nodes.total}`} detail={overview.nodes.pressure ? `${overview.nodes.pressure} under pressure` : `${overview.nodes.cordoned} cordoned`} tone={overview.nodes.notReady ? 'warning' : 'good'} icon={<Server size={17} />} href={targets.nodes ? routeHref(targets.nodes, context, namespace) : undefined} ariaLabel="Open the canonical Nodes list" />
        <MetricCard label="Pods running" value={`${overview.pods.running}`} detail={`${overview.pods.pending} pending / ${overview.pods.failed} failed`} tone={overview.pods.failed || overview.pods.pending ? 'warning' : 'good'} icon={<CircleDot size={17} />} href={targets.pods ? routeHref(targets.pods, context, namespace) : undefined} ariaLabel="Open the canonical Pods list" />
        <MetricCard label="Workload health" value={workloadHealth ? `${workloadReady}/${workloadHealth}` : '—'} detail={`${overview.workloads.jobs} jobs / ${overview.workloads.cronJobs} schedules`} tone={workloadHealth && workloadReady < workloadHealth ? 'warning' : 'good'} icon={<Layers3 size={17} />} href={targets.workloads ? routeHref(targets.workloads, context, namespace) : undefined} ariaLabel="Open the aggregate workload view for Deployments, StatefulSets, and DaemonSets" />
        <MetricCard label="Storage claims" value={`${overview.storage.bound}/${overview.storage.total}`} detail={`${overview.storage.pending} pending / ${overview.storage.lost} lost`} tone={overview.storage.pending || overview.storage.lost ? 'warning' : 'good'} icon={<HardDrive size={17} />} href={targets.storageClaims ? routeHref(targets.storageClaims, context, namespace) : undefined} ariaLabel="Open the canonical PersistentVolumeClaim list" />
      </section>

      <section className="dashboard-grid">
        <div className="panel signal-panel">
          <PanelHeading eyebrow="Signal check" title="What needs your eye" action={eventsPath ? (warningCount ? `${warningCount} warnings` : 'No warnings') : 'Events unavailable'} actionHref={eventsHref} actionAriaLabel={eventsPath ? `Open Events list for ${eventScope}` : 'Events list unavailable because the exact core v1 Events resource is not in the current catalog'} tone={eventsPath ? (warningCount ? 'warning' : 'good') : 'neutral'} />
          {overview.warnings.length ? <WarningList warnings={overview.warnings} definitions={definitions} eventsPath={eventsPath} context={context} namespace={namespace} /> : <QuietState icon={<ShieldCheck size={22} />} title="Nothing shouting" detail={eventsPath ? 'No recent Warning Events were reported.' : 'The exact core v1 Events resource is unavailable in the current API catalog.'} />}
        </div>
        <div className="panel posture-panel">
          <PanelHeading eyebrow="Cluster posture" title="Capacity & coverage" action={metricsStateLabel(overview.metricsAvailable, overview.metrics.state)} tone={metricsStateTone(overview.metricsAvailable, overview.metrics.state)} />
          <div className="posture-stack">
            <PostureRow icon={<Layers3 size={16} />} label="Namespaces" value={String(overview.namespaces)} note="scoped workspaces" href={targets.namespaces ? routeHref(targets.namespaces, context, namespace) : undefined} ariaLabel="Open the canonical Namespaces list" />
            <PostureRow icon={<Activity size={16} />} label="Restarts" value={String(overview.pods.restarting)} note="Pods with restarts" tone={overview.pods.restarting ? 'warning' : 'good'} href={targets.podRestarts ? routeHref(targets.podRestarts, context, namespace) : undefined} ariaLabel="Open the canonical Pods list to inspect restarts" />
            <PostureRow icon={<HardDrive size={16} />} label="Bound claims" value={String(overview.storage.bound)} note="PersistentVolumeClaims" href={targets.boundClaims ? routeHref(targets.boundClaims, context, namespace) : undefined} ariaLabel="Open the canonical PersistentVolumeClaim list" />
          </div>
          <div className="panel-note"><Gauge size={14} /> Native usage stays read-only and follows Kubernetes RBAC.</div>
        </div>
      </section>

      <MetricsPanel metrics={overview.metrics} metricsAvailable={overview.metricsAvailable} loading={loading} definitions={definitions} context={context} namespace={namespace} />

      <section className="dashboard-footer-line">
        <span><RefreshCw size={13} /> Refreshed {relativeTime(overview.updatedAt)}</span>
        <span><Command size={13} /> Context <strong>{overview.context.name}</strong></span>
        <span><LockKeyhole size={13} /> Dashboard is read-only</span>
      </section>
    </div>
  )
}

/** Renders the aggregate workload landing view without collapsing health into one controller kind. */
function WorkloadsPage({ context, namespace, definitions, catalogLoading, catalogError }: { context: string; namespace: string; definitions: ResourceDefinition[]; catalogLoading: boolean; catalogError: string }) {
  const targets = workloadNavigationTargets(definitions)
  const controllerTargets = targets.slice(0, 3)
  const taskTargets = targets.slice(3)
  const availableCount = targets.filter((target) => target.path).length
  const catalogLabel = catalogLoading && !definitions.length ? 'Reading catalog' : catalogError ? 'Catalog needs attention' : `${availableCount}/${targets.length} destinations`

  return (
    <div className="workload-page">
      <section className="workload-intro">
        <div>
          <div className="eyebrow"><span className="eyebrow-mark" /> Workload field / aggregate health</div>
          <h1>Workloads</h1>
          <p className="intro-copy">The overview health figure combines Deployments, StatefulSets, and DaemonSets. Use the exact resource destinations below to inspect each controller kind without losing the current context or namespace scope.</p>
        </div>
        <div className="context-stamp workload-stamp">
          <span className="stamp-label">Navigation scope</span>
          <code>{namespace === 'all' ? 'all namespaces' : namespace}</code>
          <span className="stamp-status"><span className="signal-dot signal-dot-good" /> {catalogLabel}</span>
        </div>
      </section>
      {catalogError && <div className="workload-catalog-notice"><AlertTriangle size={15} /><span>Some exact workload destinations are unavailable until API discovery succeeds: {catalogError}</span></div>}
      <section className="workload-panel-grid">
        <section className="panel workload-panel">
          <div className="panel-heading"><div><span className="panel-eyebrow">Overview health sources</span><h2>Controller kinds</h2></div><span className="panel-action panel-action-good"><span />{controllerTargets.filter((target) => target.path).length}/3 available</span></div>
          <p className="workload-panel-copy">These three Kubernetes workload kinds make up the aggregate Workload health card.</p>
          <div className="workload-target-grid">{controllerTargets.map((target) => <WorkloadDestination key={target.key} target={target} context={context} namespace={namespace} />)}</div>
        </section>
        <section className="panel workload-panel">
          <div className="panel-heading"><div><span className="panel-eyebrow">Adjacent task context</span><h2>Jobs &amp; schedules</h2></div><span className="panel-action panel-action-neutral"><span />{taskTargets.filter((target) => target.path).length}/2 available</span></div>
          <p className="workload-panel-copy">Finite Jobs and scheduled CronJobs remain available beside the controller view.</p>
          <div className="workload-target-grid">{taskTargets.map((target) => <WorkloadDestination key={target.key} target={target} context={context} namespace={namespace} />)}</div>
        </section>
      </section>
    </div>
  )
}

/** Renders one canonical workload destination or an explicit unavailable state. */
function WorkloadDestination({ target, context, namespace }: { target: WorkloadNavigationTarget; context: string; namespace: string }) {
  const icon = target.key === 'deployments' ? <Layers3 size={16} /> : target.key === 'statefulsets' ? <Database size={16} /> : target.key === 'daemonsets' ? <Zap size={16} /> : <Clock3 size={16} />
  const content = <><span className="workload-target-icon">{icon}</span><span className="workload-target-copy"><strong>{target.label}</strong><span>{target.description}</span>{!target.path && <small>Unavailable in the current API catalog</small>}</span>{target.path ? <ArrowUpRight size={15} className="workload-target-arrow" /> : <span className="workload-target-state">Unavailable</span>}</>
  if (!target.path) return <div className="workload-target workload-target-unavailable" aria-disabled="true">{content}</div>
  return <a className="workload-target" href={routeHref(target.path, context, namespace)} aria-label={`Open the canonical ${target.label} list`}>{content}</a>
}

/** Renders native node and Pod usage charts with explicit source states. */
function MetricsPanel({ metrics, metricsAvailable, loading, definitions, context, namespace }: { metrics: MetricsOverview; metricsAvailable: boolean; loading: boolean; definitions: ResourceDefinition[]; context: string; namespace: string }) {
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
          <NodeUsageChart state={metrics.nodeState} message={metrics.nodeMessage} count={metrics.nodeCount} items={metrics.nodes} definitions={definitions} context={context} namespace={namespace} />
          <PodUsageChart state={metrics.podState} message={metrics.podMessage} count={metrics.podCount} items={metrics.pods} definitions={definitions} context={context} />
        </div>
      ) : null}
      {hasMetricTimestamp(metrics.updatedAt) && <div className="metrics-panel-footer"><span><Activity size={13} /> Latest sample {relativeTime(metrics.updatedAt)}</span><span>{metrics.message || 'Native metrics are read through the local Go backend.'}</span></div>}
    </section>
  )
}

/** Identifies the additional loading state used while the overview is being refreshed. */
type MetricsDisplayState = MetricsState | 'loading'

/** Renders a node usage chart with CPU and memory capacity bars. */
function NodeUsageChart({ state, message, count, items, definitions, context, namespace }: { state: MetricsState; message?: string; count: number; items: NodeMetric[]; definitions: ResourceDefinition[]; context: string; namespace: string }) {
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
      {!showNotice && items.length ? <div className="metrics-chart-list">{items.map((item) => <NodeUsageRow key={item.name} item={item} maxCpu={maxCpu} maxMemory={maxMemory} definitions={definitions} context={context} namespace={namespace} />)}</div> : null}
    </article>
  )
}

/** Renders a Pod usage chart with relative CPU and memory bars. */
function PodUsageChart({ state, message, count, items, definitions, context }: { state: MetricsState; message?: string; count: number; items: PodMetric[]; definitions: ResourceDefinition[]; context: string }) {
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
      {!showNotice && items.length ? <div className="metrics-chart-list">{items.map((item) => <PodUsageRow key={`${item.namespace}/${item.name}`} item={item} maxCpu={maxCpu} maxMemory={maxMemory} definitions={definitions} context={context} />)}</div> : null}
    </article>
  )
}

/** Renders one node's CPU and memory utilization bars. */
function NodeUsageRow({ item, maxCpu, maxMemory, definitions, context, namespace }: { item: NodeMetric; maxCpu: number; maxMemory: number; definitions: ResourceDefinition[]; context: string; namespace: string }) {
  const cpuWidth = usageBarWidth(item.cpuPercent, item.usage.cpuCores, maxCpu)
  const memoryWidth = usageBarWidth(item.memoryPercent, item.usage.memoryBytes, maxMemory)
  const destination = overviewMetricDestination(definitions, 'node', item.name)
  const href = destination ? routeHref(destination.path, context, namespace) : undefined
  return <MetricSample href={href} ariaLabel={`Open Node ${item.name} resource detail`}><div className="metrics-row-label"><strong>{item.name}</strong><span>{formatMetricCPU(item.usage.cpuCores)} CPU / {formatMetricMemory(item.usage.memoryBytes)}</span></div><MetricBar label="CPU" width={cpuWidth} value={item.cpuPercent > 0 ? `${formatMetricPercent(item.cpuPercent)}` : formatMetricCPU(item.usage.cpuCores)} tone="copper" /><MetricBar label="MEM" width={memoryWidth} value={item.memoryPercent > 0 ? `${formatMetricPercent(item.memoryPercent)}` : formatMetricMemory(item.usage.memoryBytes)} tone="moss" /></MetricSample>
}

/** Renders one Pod's aggregated CPU and memory bars. */
function PodUsageRow({ item, maxCpu, maxMemory, definitions, context }: { item: PodMetric; maxCpu: number; maxMemory: number; definitions: ResourceDefinition[]; context: string }) {
  const podPrefix = item.namespace ? `${item.namespace}/` : ''
  const destination = overviewMetricDestination(definitions, 'pod', item.name, item.namespace)
  const href = destination?.namespace ? routeHref(destination.path, context, destination.namespace) : undefined
  return <MetricSample href={href} ariaLabel={item.namespace ? `Open Pod ${item.name} in namespace ${item.namespace} resource detail` : `Pod ${item.name} resource detail unavailable without a namespace`}><div className="metrics-row-label"><strong>{podPrefix}{item.name}</strong><span>{formatMetricCPU(item.usage.cpuCores)} CPU / {formatMetricMemory(item.usage.memoryBytes)}</span></div><MetricBar label="CPU" width={usageBarWidth(0, item.usage.cpuCores, maxCpu)} value={formatMetricCPU(item.usage.cpuCores)} tone="copper" /><MetricBar label="MEM" width={usageBarWidth(0, item.usage.memoryBytes, maxMemory)} value={formatMetricMemory(item.usage.memoryBytes)} tone="moss" /></MetricSample>
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
function MetricCard({ label, value, detail, tone, icon, href, ariaLabel }: { label: string; value: string; detail: string; tone: 'good' | 'warning' | 'neutral'; icon: ReactNode; href?: string; ariaLabel?: string }) {
  const content = <><div className="metric-top"><span className="metric-icon">{icon}</span><span className="metric-kicker">{label}</span>{href && <ArrowUpRight size={14} className="metric-arrow" />}</div><div className="metric-value">{value}</div><div className="metric-detail">{detail}</div></>
  if (!href) return <article className={`metric-card metric-${tone}`}>{content}</article>
  return <a className={`metric-card metric-card-link metric-${tone}`} href={href} aria-label={ariaLabel || `Open ${label}`}>{content}</a>
}

/** Renders a section heading with a compact state marker. */
function PanelHeading({ eyebrow, title, action, tone, actionHref, actionAriaLabel }: { eyebrow: string; title: string; action: string; tone: 'good' | 'warning' | 'neutral'; actionHref?: string; actionAriaLabel?: string }) {
  const actionContent = <><span aria-hidden="true" />{action}</>
  return (
    <div className="panel-heading">
      <div><span className="panel-eyebrow">{eyebrow}</span><h2>{title}</h2></div>
      {actionHref ? <a className={`panel-action panel-action-${tone} panel-action-link`} href={actionHref} aria-label={actionAriaLabel || action}>{actionContent}</a> : <span className={`panel-action panel-action-${tone}`}>{actionContent}</span>}
    </div>
  )
}

/** Renders recent warning Events in a readable dashboard list. */
function WarningList({ warnings, definitions, eventsPath, context, namespace }: { warnings: ClusterOverview['warnings']; definitions: ResourceDefinition[]; eventsPath?: string; context: string; namespace: string }) {
  return (
    <div className="warning-list">
      {warnings.map((warning, index) => (
        <WarningEventRow key={`${warning.namespace || ''}/${warning.name || ''}/${warning.reason}-${warning.object}-${index}`} warning={warning} definitions={definitions} eventsPath={eventsPath} context={context} namespace={namespace} index={index} />
      ))}
    </div>
  )
}

/** Renders one warning Event as an exact detail link, a bounded list fallback, or an unavailable state. */
function WarningEventRow({ warning, definitions, eventsPath, context, namespace, index }: { warning: EventSummary; definitions: ResourceDefinition[]; eventsPath?: string; context: string; namespace: string; index: number }) {
  const detailPath = overviewEventDetailPath(definitions, warning)
  const targetPath = detailPath || eventsPath
  const targetNamespace = warning.namespace || namespace
  const targetHref = targetPath ? routeHref(targetPath, context, targetNamespace) : undefined
  const targetLabel = detailPath
    ? `Open Warning Event ${warning.name} in namespace ${warning.namespace}`
    : eventsPath
      ? `Open Events list for ${targetNamespace === 'all' ? 'all namespaces' : `namespace ${targetNamespace}`}; this Event identity is incomplete`
      : 'Warning Event detail unavailable because the exact core v1 Events resource is not in the current catalog'
  const targetNote = detailPath ? undefined : eventsPath ? 'Event identity incomplete; open the bounded Events list' : 'Event detail unavailable'
  const content = <><span className="warning-index">{String(index + 1).padStart(2, '0')}</span><div className="warning-main"><strong>{warning.reason || 'Warning Event'}</strong><span>{warning.message || 'No message provided.'}</span>{targetNote && <small className="warning-target-note">{targetNote}</small>}</div><div className="warning-meta"><code>{warning.object || 'cluster'}</code><span>{relativeTime(warning.lastSeen)}</span></div></>
  if (!targetHref) return <div className="warning-row warning-row-unavailable" aria-disabled="true" aria-label={targetLabel}>{content}</div>
  return <a className={`warning-row warning-row-link ${detailPath ? '' : 'warning-row-fallback'}`} href={targetHref} aria-label={targetLabel}>{content}</a>
}

/** Renders a single posture statistic row. */
function PostureRow({ icon, label, value, note, tone = 'neutral', href, ariaLabel }: { icon: ReactNode; label: string; value: string; note: string; tone?: 'good' | 'warning' | 'neutral'; href?: string; ariaLabel?: string }) {
  const content = <><span className={`posture-icon posture-${tone}`}>{icon}</span><div><strong>{label}</strong><span>{note}</span></div><b>{value}</b>{href && <ArrowUpRight size={14} className="posture-arrow" />}</>
  if (!href) return <div className="posture-row">{content}</div>
  return <a className="posture-row posture-row-link" href={href} aria-label={ariaLabel || `Open ${label}`}>{content}</a>
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

/** Renders explicit discovery health while keeping usable catalog rows visible. */
function DiscoveryNotice({ catalog, loading, error, onRetry }: { catalog: ResourceCatalogResponse | null; loading: boolean; error: string; onRetry: () => void }) {
  const issueCount = catalog?.issues?.length ?? 0
  let title = 'API catalog is partial'
  let detail = `${issueCount} discovery source${issueCount === 1 ? '' : 's'} need attention. Known resources remain available.`
  let tone = 'partial'
  if (loading) {
    title = 'Reading the Kubernetes API catalog'
    detail = 'Loading every served API group and version. The catalog will not be reduced to a preferred-version shortcut.'
    tone = 'loading'
  } else if (error) {
    title = 'API catalog could not be loaded'
    detail = 'The current resource route stays unresolved until discovery succeeds. Retry after the API server or permissions recover.'
    tone = 'error'
  } else if (catalog?.state === 'permission-denied') {
    title = 'API catalog access is restricted'
    detail = 'Kubernetes RBAC denied discovery for the available API sources. Retry after access changes.'
    tone = 'error'
  } else if (catalog?.state === 'unavailable') {
    title = 'Some Kubernetes APIs are unavailable'
    detail = 'The catalog retained any successful discovery results. Retry to check unavailable API groups again.'
    tone = 'unavailable'
  } else if (catalog?.state === 'error') {
    title = 'API discovery returned an error'
    detail = 'Some group or version metadata could not be read. Retry to refresh the catalog.'
    tone = 'error'
  }
  return <div className={`discovery-notice discovery-notice-${tone}`} role="status"><span className="discovery-notice-mark">{loading ? <LoaderCircle size={15} className="spin" /> : <AlertTriangle size={15} />}</span><div><strong>{title}</strong><span>{detail}</span>{catalog?.issues?.slice(0, 3).map((issue, index) => <code key={`${issue.group || 'core'}-${issue.version || 'unknown'}-${index}`}>{issue.message}</code>)}{error && <code>{error}</code>}</div>{!loading && <button type="button" className="secondary-button" onClick={onRetry}><RefreshCw size={14} /> Retry discovery</button>}</div>
}

/** Renders the complete grouped API resource catalog without truncating large CRD sets. */
function ResourceCatalogPage({ catalog, loading, error, onRetry, onOpen }: { catalog: ResourceCatalogResponse | null; loading: boolean; error: string; onRetry: () => void; onOpen: (definition: ResourceDefinition) => void }) {
  const [filter, setFilter] = useState('')
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({})

  if (loading && !catalog) return <LoadingState label="Reading the Kubernetes API catalog" />
  if (error && !catalog) return <CatalogState title="Resource catalog unavailable" detail={error} onRetry={onRetry} />
  if (!catalog) return <CatalogState title="No API catalog loaded" detail="Choose a cluster context to read its discoverable resources." onRetry={onRetry} />
  const filteredDefinitions = filterResourceCatalog(catalog.items, filter)
  const groups = groupResourceCatalog(filteredDefinitions)
  const filterActive = filter.trim().length > 0

  /** Toggles one API group's local expanded state without persisting it. */
  function toggleGroup(group: string) {
    setCollapsedGroups((current) => ({ ...current, [group]: current[group] === false }))
  }

  return <div className="catalog-page">
    <section className="catalog-intro">
      <div><div className="eyebrow"><span className="eyebrow-mark" /> Kubernetes API / discovery</div><h1>Resource catalog</h1><p className="intro-copy">Every discovered top-level resource, served version, namespace scope, and subresource capability in this cluster. Short links are only retained when their resource name is unambiguous.</p></div>
      <div className="catalog-stamp"><span className="stamp-label">Catalog coverage</span><strong>{catalog.items.length}</strong><code>{catalog.state === 'available' ? 'all discovery sources online' : `${catalog.issues?.length ?? 0} source issues`}</code></div>
    </section>
    {catalog.issues?.length ? <div className="catalog-issues">{catalog.issues.map((issue, index) => <div className={`catalog-issue catalog-issue-${issue.state}`} key={`${issue.group || 'core'}-${issue.version || 'unknown'}-${index}`}><AlertTriangle size={14} /><div><strong>{issue.group ? `${issue.group}/${issue.version}` : issue.version || 'API discovery'}</strong><span>{issue.message}</span></div><button type="button" className="text-button" onClick={onRetry}>Retry</button></div>)}</div> : null}
    <div className="catalog-filter-bar">
      <label className="catalog-filter-field" htmlFor="catalog-resource-filter">
        <Search size={14} aria-hidden="true" />
        <span className="sr-only">Filter API resource catalog</span>
        <input id="catalog-resource-filter" type="search" value={filter} onChange={(event) => setFilter(event.target.value)} placeholder="Search group, version, resource, kind..." />
      </label>
      {filterActive ? <button type="button" className="catalog-clear-filter" onClick={() => setFilter('')}><X size={13} /> Clear filter</button> : null}
      <span className="catalog-filter-count" role="status" aria-live="polite"><strong>{filteredDefinitions.length}</strong>{filterActive ? ` of ${catalog.items.length} resources match` : ` resource${catalog.items.length === 1 ? '' : 's'}`}<small>{groups.length} API group{groups.length === 1 ? '' : 's'} shown</small></span>
    </div>
    {!groups.length ? <div className="catalog-empty catalog-filter-empty" role={filterActive ? 'status' : undefined}>{filterActive ? <Search size={22} /> : <Box size={22} />}<strong>{filterActive ? `No resources match “${filter}”` : 'No top-level resources were discovered'}</strong><span>{filterActive ? 'Try a group, version, resource name, kind, alias, or category.' : 'Retry discovery when the Kubernetes API is reachable.'}</span>{filterActive ? <button type="button" className="secondary-button" onClick={() => setFilter('')}><X size={14} /> Clear filter</button> : <button type="button" className="secondary-button" onClick={onRetry}><RefreshCw size={14} /> Refresh catalog</button>}</div> : <div className="catalog-groups" id="catalog-groups">{groups.map(({ group, versions }, groupIndex) => {
      const groupLabel = group || 'Core'
      const expanded = filterActive || collapsedGroups[group] !== false
      const headingID = `catalog-group-heading-${groupIndex}`
      const contentID = `catalog-group-content-${groupIndex}`
      return <section className="catalog-group" key={group} aria-labelledby={headingID}><div className="catalog-group-heading"><span className="panel-eyebrow">API group</span><h2 id={headingID}>{groupLabel}</h2><code>{versions.reduce((total, version) => total + version.definitions.length, 0)} resources</code><button type="button" className="catalog-group-toggle" aria-expanded={expanded} aria-controls={contentID} aria-label={filterActive ? `${groupLabel} API group is expanded while filtering` : `${expanded ? 'Collapse' : 'Expand'} ${groupLabel} API group`} disabled={filterActive} onClick={() => toggleGroup(group)}><ChevronDown size={16} aria-hidden="true" /></button></div><div id={contentID} hidden={!expanded}>{versions.map(({ version, definitions }) => <div className="catalog-version" key={`${group}/${version}`}><div className="catalog-version-heading"><strong>{version}</strong><span>{definitions.length} top-level resource{definitions.length === 1 ? '' : 's'}</span></div><div className="catalog-table"><div className="catalog-table-heading"><span>Resource</span><span>Kind</span><span>Scope</span><span>Discovery</span><span>Capabilities</span></div>{definitions.map((definition) => <CatalogResourceRow key={`${definition.group}/${definition.version}/${definition.resource}`} definition={definition} onOpen={onOpen} />)}</div></div>)}</div></section>
    })}</div>}
  </div>
}

/** Renders one complete GVR row with verbs, aliases, categories, and subresources. */
function CatalogResourceRow({ definition, onOpen }: { definition: ResourceDefinition; onOpen: (definition: ResourceDefinition) => void }) {
  const scope = definition.scope || (definition.namespaced ? 'Namespaced' : 'Cluster')
  const subresources = definition.subresources ?? []
  const custom = definition.customResourceCapabilities
  return <button type="button" className="catalog-row" onClick={() => onOpen(definition)}><span className="catalog-resource-cell"><strong>{definition.resource}</strong><code>{definition.group ? `${definition.group}/${definition.version}` : definition.version}</code></span><span className="catalog-kind-cell">{definition.kind || 'Unknown kind'}</span><span><span className={`catalog-scope catalog-scope-${scope.toLowerCase()}`}>{scope}</span></span><span className="catalog-meta-cell"><span className="catalog-chip-list">{(definition.verbs ?? []).map((verb) => <code key={verb}>{verb}</code>)}</span>{definition.shortNames?.length ? <small>aliases: {definition.shortNames.join(', ')}</small> : null}{definition.categories?.length ? <small>categories: {definition.categories.join(', ')}</small> : null}{custom ? <><small>CRD / {customResourceCapabilityLabel(custom)}</small><span className="catalog-chip-list">{custom.statusSubresource && <code>status</code>}{custom.scaleSubresource && <code>scale</code>}{custom.printerColumns?.length ? <code>{custom.printerColumns.length} printer columns</code> : null}</span></> : null}</span><span className="catalog-meta-cell">{subresources.length ? <span className="catalog-chip-list">{subresources.map((subresource) => <code key={subresource.name}>{subresource.name}</code>)}</span> : <small>No discovered subresources</small>}{subresources.map((subresource) => <small key={`${subresource.name}-verbs`}>{subresource.name}: {(subresource.verbs ?? []).join(', ') || 'verbs not advertised'}</small>)}{custom?.printerColumnWarnings?.length ? <small>Some printer columns use the generic table fallback.</small> : null}</span></button>
}

/** Renders a retryable resource-catalog failure without substituting a guessed resource. */
function CatalogState({ title, detail, onRetry }: { title: string; detail: string; onRetry: () => void }) {
  return <div className="state-page"><div className="state-card"><span className="state-icon state-icon-warning"><AlertTriangle size={22} /></span><span className="eyebrow"><span className="eyebrow-mark" /> API catalog</span><h1>{title}</h1><p>{detail}</p><button type="button" className="secondary-button" onClick={onRetry}><RefreshCw size={15} /> Retry discovery</button></div></div>
}

/** Renders the explicit-submit global search page and its revision-bound saved views. */
function GlobalSearchPage({ context, namespace, revision, searchKey, definitions, catalog, catalogLoading, onNamespace, onOpen }: { context: string; namespace: string; revision: number; searchKey: string; definitions: ResourceDefinition[]; catalog: ResourceCatalogResponse | null; catalogLoading: boolean; onNamespace: (namespace: string) => void; onOpen: (result: SearchResult) => void }) {
  const initialQuery = parseSearchQuery(searchKey)
  const [query, setQuery] = useState<SearchQuery>(() => initialQuery)
  const [submittedQuery, setSubmittedQuery] = useState<SearchQuery | null>(() => hasSearchFilters(initialQuery) ? initialQuery : null)
  const [response, setResponse] = useState<SearchResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [savedViews, setSavedViews] = useState<SavedSearchView[]>(() => {
    const storage = browserSavedViewStorage()
    return storage ? loadSavedSearchViews(storage) : []
  })
  const [viewName, setViewName] = useState('')
  const [viewNotice, setViewNotice] = useState('')
  const searchAbortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    const nextQuery = parseSearchQuery(searchKey)
    const nextSubmittedQuery = hasSearchFilters(nextQuery) ? nextQuery : null
    searchAbortRef.current?.abort()
    searchAbortRef.current = null
    setQuery((current) => searchQueryEquals(current, nextQuery) ? current : nextQuery)
    setSubmittedQuery((current) => {
      if (!nextSubmittedQuery) return current === null ? current : null
      return current && searchQueryEquals(current, nextSubmittedQuery) ? current : nextSubmittedQuery
    })
    setResponse(null)
    setLoading(false)
    setError('')
    setViewNotice('')
  }, [searchKey])

  useEffect(() => {
    searchAbortRef.current?.abort()
    searchAbortRef.current = null
    setQuery((current) => current.namespace === namespace ? current : { ...current, namespace })
    setSubmittedQuery((current) => current && current.namespace === namespace ? current : null)
    setResponse(null)
    setError('')
  }, [namespace])

  useEffect(() => {
    if (!context || !submittedQuery) {
      setLoading(false)
      return
    }
    const controller = new AbortController()
    searchAbortRef.current = controller
    setLoading(true)
    setError('')
    fetchGlobalSearch(context, submittedQuery, controller.signal)
      .then((nextResponse) => {
        if (controller.signal.aborted) return
        setResponse(nextResponse)
      })
      .catch((loadError: unknown) => {
        if (controller.signal.aborted || (loadError as Error).name === 'AbortError') return
        setResponse(null)
        setError('Global search could not be completed. Kubernetes error details are hidden by the local server; retry the search.')
      })
      .finally(() => !controller.signal.aborted && setLoading(false))
    return () => {
      controller.abort()
      if (searchAbortRef.current === controller) searchAbortRef.current = null
    }
  }, [context, revision, submittedQuery])

  /** Updates one filter and immediately records the deep-linkable URL without issuing a request. */
  function updateQuery(field: keyof SearchQuery, value: string) {
    const next = normalizeSearchQuery({ ...query, [field]: value })
    setQuery(next)
    if (field === 'namespace') onNamespace(next.namespace)
    window.history.replaceState({}, '', searchHref(context, next))
  }

  /** Submits the current filter model as the next bounded Kubernetes search. */
  function submitSearch() {
    setViewNotice('')
    setError('')
    setResponse(null)
    setSubmittedQuery({ ...query })
  }

  /** Clears search filters while retaining the active namespace scope. */
  function clearSearch() {
    const next = emptySearchQuery(namespace)
    setQuery(next)
    setSubmittedQuery(null)
    setResponse(null)
    setError('')
    setViewNotice('')
    window.history.replaceState({}, '', searchHref(context, next))
  }

  /** Stores the current filters only when the active context and catalog are available. */
  function saveView() {
    const storage = browserSavedViewStorage()
    if (!storage) {
      setViewNotice('Browser storage is unavailable; this view was not saved.')
      return
    }
    if (!context || !catalog || catalogLoading || catalog.state !== 'available' || !definitions.length) {
      setViewNotice('Wait for the current API catalog before saving a view.')
      return
    }
    const view = createSavedSearchView(viewName, context, revision, query, definitions)
    const next = upsertSavedSearchView(savedViews, view)
    const persisted = persistSavedSearchViews(storage, next)
    if (!persisted.some((candidate) => candidate.id === view.id)) {
      setViewNotice('Browser storage could not retain this view within its fixed limit.')
      return
    }
    setSavedViews(persisted)
    setViewName('')
    setViewNotice(`Saved “${view.name}” for ${context} at revision ${revision}.`)
  }

  /** Applies a view only after context, revision, and catalog identity validation succeeds. */
  function applyView(view: SavedSearchView) {
    if (!catalog || catalogLoading || catalog.state !== 'available' || !isSavedSearchViewApplicable(view, context, revision, definitions)) {
      setViewNotice(`“${view.name}” is stale or no longer matches this context's discovered resources. Delete it or save a new view.`)
      return
    }
    const next = normalizeSearchQuery(view.query)
    setQuery(next)
    setSubmittedQuery(next)
    if (next.namespace !== namespace) onNamespace(next.namespace)
    window.history.replaceState({}, '', searchHref(context, next))
    setViewNotice(`Applied “${view.name}”.`)
  }

  /** Deletes one local view and persists the remaining bounded collection. */
  function deleteView(view: SavedSearchView) {
    const next = removeSavedSearchView(savedViews, view.id)
    const storage = browserSavedViewStorage()
    setSavedViews(storage ? persistSavedSearchViews(storage, next) : next)
    setViewNotice(`Deleted “${view.name}”.`)
  }

  const groupOptions = searchGroupOptions(definitions)
  const kindOptions = searchKindOptions(definitions, query.group)
  const resourceOptions = searchResourceOptions(definitions, query.group, query.kind)
  const kindMatchesCatalog = !query.kind || searchDefinitionCandidates(definitions, { ...emptySearchQuery(namespace), group: query.group, kind: query.kind }).length > 0
  const resourceMatchesCatalog = !query.resource || searchDefinitionCandidates(definitions, { ...emptySearchQuery(namespace), group: query.group, kind: query.kind, resource: query.resource }).length > 0
  const kindNoMatch = Boolean(query.kind && definitions.length && !kindMatchesCatalog)
  const resourceNoMatch = Boolean(query.resource && definitions.length && !resourceMatchesCatalog)
  const kindHint = catalogLoading && !definitions.length ? 'Catalog suggestions are loading.' : kindNoMatch ? 'No loaded Kind matches this API group. Your free-form value stays unchanged.' : kindOptions.length ? `${kindOptions.length} catalog suggestion${kindOptions.length === 1 ? '' : 's'}; group/version context is shown.` : definitions.length ? 'No Kind suggestions for this group. Custom kinds remain allowed.' : 'No catalog suggestions loaded. Custom kinds remain allowed.'
  const resourceHint = catalogLoading && !definitions.length ? 'Catalog suggestions are loading.' : resourceNoMatch ? 'No loaded Resource matches this API group and Kind. Your free-form value stays unchanged.' : resourceOptions.length ? `${resourceOptions.length} catalog suggestion${resourceOptions.length === 1 ? '' : 's'}; group/version/Kind context is shown.` : definitions.length ? 'No Resource suggestions match these filters. Custom resources remain allowed.' : 'No catalog suggestions loaded. Custom resources remain allowed.'
  return (
    <div className="search-page">
      <section className="search-intro">
        <div>
          <div className="eyebrow"><span className="eyebrow-mark" /> Global field scan / read-only</div>
          <h1>Search the cluster</h1>
          <p className="intro-copy">Find named objects across discovered API groups without guessing a short resource route. Every result keeps its complete group, version, and resource identity.</p>
        </div>
        <div className="search-stamp">
          <span className="stamp-label">Current search scope</span>
          <strong>{context || 'No context selected'}</strong>
          <code>{namespace === 'all' ? 'all namespaces' : namespace} / kubeconfig rev {revision || '—'}</code>
          <span className="stamp-status"><span className="signal-dot signal-dot-good" /> local backend / bounded scan</span>
        </div>
      </section>

      <div className="search-layout">
        <main className="search-main">
          <section className="search-form-panel panel">
            <div className="search-form-heading"><div><span className="panel-eyebrow">Operator query</span><h2>Set the field coordinates</h2><p>Typing changes only the URL. Submit deliberately to scan at most 24 sources and 200 summaries.</p></div><span className="search-budget-chip"><Search size={13} /> 24 sources / 6 concurrent</span></div>
            <form className="search-form" onSubmit={(event) => { event.preventDefault(); submitSearch() }}>
              <label className="search-primary-field"><Search size={17} /><span className="sr-only">Search text</span><input value={query.query} onChange={(event) => updateQuery('query', event.target.value)} placeholder="Search kind, resource, namespace, or object name" /><kbd>submit</kbd></label>
              <div className="search-filter-grid">
                <label className="search-control"><span>API group</span><select value={query.group} onChange={(event) => updateQuery('group', event.target.value)}><option value="">All groups</option>{groupOptions.map((group) => <option value={group.value} key={group.value}>{group.label}</option>)}</select></label>
                <label className={`search-control search-control-autocomplete${kindNoMatch ? ' search-control-invalid' : ''}`}><span>Kind</span><input list="global-search-kind-options" aria-describedby="global-search-kind-help" aria-autocomplete="list" autoComplete="off" value={query.kind} onChange={(event) => updateQuery('kind', event.target.value)} placeholder="Deployment" /><datalist id="global-search-kind-options">{kindOptions.map((option) => <option key={option.value} value={option.value} label={option.label} />)}</datalist><small id="global-search-kind-help" className={kindNoMatch ? 'search-control-hint search-control-hint-warning' : 'search-control-hint'} role="status">{kindHint}</small></label>
                <label className={`search-control search-control-autocomplete${resourceNoMatch ? ' search-control-invalid' : ''}`}><span>Resource</span><input list="global-search-resource-options" aria-describedby="global-search-resource-help" aria-autocomplete="list" autoComplete="off" value={query.resource} onChange={(event) => updateQuery('resource', event.target.value)} placeholder="deployments" /><datalist id="global-search-resource-options">{resourceOptions.map((option) => <option key={option.value} value={option.value} label={option.label} />)}</datalist><small id="global-search-resource-help" className={resourceNoMatch ? 'search-control-hint search-control-hint-warning' : 'search-control-hint'} role="status">{resourceHint}</small></label>
                <label className="search-control"><span>Namespace scope</span><input value={query.namespace === 'all' ? '' : query.namespace} onChange={(event) => updateQuery('namespace', event.target.value || 'all')} placeholder="all namespaces" /></label>
                <label className="search-control"><span>Name / exact field selector</span><input value={query.name} onChange={(event) => updateQuery('name', event.target.value)} placeholder="api-server" /></label>
                <label className="search-control search-control-wide"><span>Label selector / sent to Kubernetes</span><input value={query.labelSelector} onChange={(event) => updateQuery('labelSelector', event.target.value)} placeholder="app=api,track=stable" /></label>
              </div>
              <div className="search-form-actions"><span>Empty searches are allowed only by explicit submit and remain bounded.</span><div><button type="button" className="text-button" onClick={clearSearch}>Clear filters</button><button type="submit" className="primary-button" disabled={!context}><Search size={15} /> Run search</button></div></div>
            </form>
          </section>

          {viewNotice && <div className="search-view-notice" role="status"><Bookmark size={14} />{viewNotice}</div>}
          {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
          {response && <SearchResponseNotice response={response} />}
          {loading && !response && <div className="search-loading"><LoaderCircle size={18} className="spin" /> Reading bounded search sources</div>}
          {loading && response && <div className="search-refresh-strip"><LoaderCircle size={14} className="spin" /> Refreshing this submitted search; the previous bounded result remains visible.</div>}
          {response && <SearchSourcesPanel sources={response.sources} issues={response.issues} />}
          {response && response.items.length > 0 && <SearchResultTable items={response.items} onOpen={onOpen} />}
          {response && !response.items.length && !loading && <SearchNoResults response={response} />}
          {!response && !loading && <div className="search-landing panel"><span className="search-landing-mark"><BookmarkPlus size={21} /></span><strong>Ready for a deliberate scan</strong><span>Use a name, kind, resource, namespace, or label selector, then run the search. No Kubernetes list calls happen while you type.</span></div>}
        </main>

        <aside className="saved-views-panel panel">
          <div className="saved-views-heading"><div><span className="panel-eyebrow">Local bookmarks</span><h2>Saved views</h2></div><span className="saved-view-count">{savedViews.length}/12</span></div>
          <p className="saved-views-copy">Views stay in this browser and are bound to <strong>{context || 'the active context'}</strong> / revision <strong>{revision || '—'}</strong>. Stale views never run silently.</p>
          <div className="saved-view-create"><input value={viewName} onChange={(event) => setViewName(event.target.value)} placeholder="Name this filter set" maxLength={64} /><button type="button" className="secondary-button" onClick={saveView} disabled={!context || catalogLoading}><BookmarkPlus size={14} /> Save</button></div>
          {!savedViews.length ? <div className="saved-views-empty"><Bookmark size={18} /><span>No saved field views yet.</span></div> : <div className="saved-view-list">{savedViews.map((view) => {
            const state: SavedViewState | 'checking' = catalogLoading || !catalog ? 'checking' : catalog.state !== 'available' ? 'incompatible' : savedSearchViewState(view, context, revision, definitions)
            const applicable = state === 'current'
            return <div className={`saved-view-row saved-view-row-${state}`} key={view.id}><div className="saved-view-row-top"><strong>{view.name}</strong><button type="button" className="icon-button" onClick={() => deleteView(view)} aria-label={`Delete saved view ${view.name}`} title="Delete saved view"><Trash2 size={14} /></button></div><span>{savedViewStateLabel(state)} / {view.context} / rev {view.revision}</span><code>{savedViewQueryLabel(view.query)}</code><button type="button" className="text-button saved-view-apply" onClick={() => applyView(view)} disabled={!applicable}>{applicable ? 'Apply view' : state === 'checking' ? 'Checking catalog' : 'Cannot apply safely'}</button></div>
          })}</div>}
          <div className="saved-views-footer"><LockKeyhole size={13} /> Local only / no search index / fixed browser storage</div>
        </aside>
      </div>
    </div>
  )
}

/** Renders the aggregate search state without exposing backend Kubernetes error text. */
function SearchResponseNotice({ response }: { response: SearchResponse }) {
  if (response.state === 'available' && !response.truncated && !response.issues?.length) return null
  const title = response.state === 'partial' ? 'Partial result set' : response.state === 'permission-denied' ? 'Search access is restricted' : response.state === 'unavailable' ? 'Some search APIs are unavailable' : response.state === 'error' ? 'Search sources returned errors' : 'Search returned no matches'
  const detail = response.state === 'partial' ? `${response.items.length} safe result${response.items.length === 1 ? '' : 's'} returned. ${response.truncated ? 'The fixed source or result cap was reached. ' : ''}Review source states below before treating this as complete.` : response.state === 'permission-denied' ? 'Kubernetes RBAC denied every source that was queried. No raw API error body is shown.' : response.state === 'unavailable' ? 'One or more Kubernetes APIs did not respond or are not currently served. Retry when the cluster recovers.' : response.state === 'error' ? 'One or more source reads failed. Successful sources remain visible and each issue is classified below.' : 'The bounded scan completed without a matching object.'
  return <div className={`search-response-notice search-response-notice-${response.state}`} role="status"><span className="search-response-icon">{response.state === 'partial' ? <AlertTriangle size={16} /> : response.state === 'permission-denied' ? <LockKeyhole size={16} /> : response.state === 'available' ? <RefreshCw size={16} /> : <AlertTriangle size={16} />}</span><div><strong>{title}</strong><span>{detail}</span>{response.issues?.slice(0, 3).map((issue, index) => <code key={`${issue.group || 'core'}-${issue.version || 'unknown'}-${index}`}>{issue.message}</code>)}</div></div>
}

/** Renders per-source RBAC, unavailable, empty, and error outcomes for one search. */
function SearchSourcesPanel({ sources, issues }: { sources: SearchSource[]; issues?: SearchResponse['issues'] }) {
  if (!sources.length && !issues?.length) return null
  return <section className="search-sources panel"><div className="search-sources-heading"><div><span className="panel-eyebrow">Source ledger</span><h2>Where the result came from</h2></div><span>{sources.length} queried</span></div>{issues?.length ? <div className="search-catalog-issues">{issues.map((issue, index) => <div key={`${issue.group || 'core'}-${issue.version || 'unknown'}-${index}`}><AlertTriangle size={13} /><span>{issue.message}</span></div>)}</div> : null}<div className="search-source-list">{sources.map((source) => <div className={`search-source-row search-source-row-${source.state}`} key={`${source.group}/${source.version}/${source.resource}`}><span className="search-source-dot" /><div><strong>{source.kind || source.resource}</strong><code>{source.group ? `${source.group}/${source.version}` : source.version} / {source.resource}</code></div><span className="search-source-result-count">{source.resultCount} result{source.resultCount === 1 ? '' : 's'}</span><span className="search-source-state">{searchSourceStateLabel(source.state)}</span>{source.issue && <span className="search-source-issue">{source.issue}</span>}</div>)}</div></section>
}

/** Renders bounded safe summaries with canonical full-GVR navigation. */
function SearchResultTable({ items, onOpen }: { items: SearchResult[]; onOpen: (result: SearchResult) => void }) {
  return <section className="search-results panel"><div className="search-results-heading"><div><span className="panel-eyebrow">Safe coordinates</span><h2>Search results</h2></div><span>{items.length} returned</span></div><div className="search-results-wrap"><table className="search-results-table"><thead><tr><th>Object</th><th>API identity</th><th>Namespace</th><th>State</th><th>Labels</th><th /></tr></thead><tbody>{items.map((item) => <tr key={searchResultKey(item)} onClick={() => onOpen(item)}><td><button type="button" className="search-result-name" onClick={(event) => { event.stopPropagation(); onOpen(item) }}>{item.name || 'Unnamed'}</button><span className="search-result-kind">{item.kind}</span></td><td><code>{item.group ? `${item.group}/${item.version}` : item.version}</code><span>{item.resource}</span></td><td>{item.namespace || 'cluster'}</td><td><span className={`status-pill status-${resourceStatusTone(item.status)}`}><span />{item.status || 'Unknown'}{item.ready ? ` / ${item.ready}` : ''}</span></td><td><div className="label-stack">{Object.entries(item.labels ?? {}).slice(0, 2).map(([key, value]) => <span key={key}>{key}={value}</span>)}{!Object.keys(item.labels ?? {}).length && <span>—</span>}</div></td><td><ArrowUpRight size={15} className="search-result-open" /></td></tr>)}</tbody></table></div><div className="search-results-footer"><span><Search size={13} /> Results contain coordinates and summaries only; open YAML/detail for the complete generic fallback.</span><span>Click a row to open its canonical route.</span></div></section>
}

/** Renders the explicit empty, denied, unavailable, or error search state. */
function SearchNoResults({ response }: { response: SearchResponse }) {
  const title = response.state === 'permission-denied' ? 'Search is blocked by Kubernetes RBAC' : response.state === 'unavailable' ? 'Search APIs are unavailable' : response.state === 'error' ? 'Search did not complete cleanly' : response.state === 'partial' ? 'No matches in the scanned sources' : 'No matching resources'
  const detail = response.state === 'permission-denied' ? 'This identity could not list any searched source. Review source access and retry.' : response.state === 'unavailable' ? 'The local backend could not read enough API sources to complete the scan. Retry when the cluster is reachable.' : response.state === 'error' ? 'No safe result was returned from the completed source calls. Review the source ledger and retry.' : response.state === 'partial' ? 'The search budget or discovery health limited coverage. Refine the filters or retry for a fresh bounded scan.' : 'The bounded scan completed successfully. Try a different name, exact kind/resource, namespace, or label selector.'
  return <div className="search-no-results"><span className="search-no-results-icon">{response.state === 'permission-denied' ? <LockKeyhole size={20} /> : response.state === 'empty' ? <Box size={20} /> : <AlertTriangle size={20} />}</span><strong>{title}</strong><span>{detail}</span></div>
}

/** Converts one saved-view compatibility state into a compact operator label. */
function savedViewStateLabel(state: SavedViewState | 'checking'): string {
  if (state === 'current') return 'Ready'
  if (state === 'checking') return 'Checking'
  if (state === 'stale-context') return 'Other context'
  if (state === 'stale-revision') return 'Stale revision'
  return 'Catalog changed'
}

/** Converts one source state into the explicit label shown in the source ledger. */
function searchSourceStateLabel(state: SearchSource['state']): string {
  if (state === 'permission-denied') return 'RBAC denied'
  if (state === 'unavailable') return 'Unavailable'
  if (state === 'error') return 'Read error'
  if (state === 'empty') return 'No matches'
  return 'Read'
}

/** Formats safe saved-view filters without embedding a potentially long selector in the UI. */
function savedViewQueryLabel(query: SearchQuery): string {
  const parts = [query.query && `q:${query.query}`, query.kind && `kind:${query.kind}`, query.resource && `resource:${query.resource}`, query.namespace !== 'all' && `ns:${query.namespace}`, query.name && `name:${query.name}`, query.labelSelector && `labels:${query.labelSelector}`].filter(Boolean)
  return parts.join(' / ') || 'all discovered resources'
}

/** Returns browser localStorage only when the local environment exposes it safely. */
function browserSavedViewStorage(): Storage | null {
  try {
    return window.localStorage
  } catch {
    return null
  }
}

/** Renders the read-only local mutation ledger with bounded search and outcome filters. */
function MutationHistoryPage({ contexts, selectedContext, activeContext, revision }: { contexts: ContextsResponse; selectedContext: string; activeContext?: ContextSummary; revision: number }) {
  const [response, setResponse] = useState<AuditResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<'all' | 'success' | 'failed'>('all')
  const [executionFilter, setExecutionFilter] = useState<'all' | 'dry-run' | 'applied'>('all')
  const [contextFilter, setContextFilter] = useState('all')
  const [resourceFilter, setResourceFilter] = useState('all')
  const [dateFilter, setDateFilter] = useState<'all' | 'today' | '7d' | '30d'>('all')
  const [refreshNonce, setRefreshNonce] = useState(0)

  useEffect(() => {
    let active = true
    const load = () => {
      setLoading(true)
      fetchAudit()
        .then((data) => {
          if (!active) return
          setResponse(data)
          setError(false)
        })
        .catch(() => active && setError(true))
        .finally(() => active && setLoading(false))
    }
    load()
    const interval = window.setInterval(load, 30000)
    return () => {
      active = false
      window.clearInterval(interval)
    }
  }, [revision, refreshNonce])

  const entries = response?.items ?? []
  const contextOptions = Array.from(new Set([...contexts.contexts.map((item) => item.name), ...entries.map((entry) => entry.context).filter(Boolean)])).sort()
  const resourceOptions = Array.from(new Map(entries.map((entry) => [auditResourceKey(entry), auditResourceLabel(entry)])).entries()).sort((left, right) => left[1].localeCompare(right[1]))
  const disconnected = contexts.contexts.length === 0 || !activeContext
  const disconnectedContext = selectedContext || 'Cluster context'
  const contextOptionKey = contextOptions.join('\u0000')
  const normalizedSearch = search.trim().toLowerCase()
  const filteredEntries = entries.filter((entry) => {
    if (statusFilter === 'success' && !entry.success) return false
    if (statusFilter === 'failed' && entry.success) return false
    if (executionFilter === 'dry-run' && !entry.dryRun) return false
    if (executionFilter === 'applied' && entry.dryRun) return false
    if (contextFilter !== 'all' && entry.context !== contextFilter) return false
    if (resourceFilter !== 'all' && auditResourceKey(entry) !== resourceFilter) return false
    if (!auditDateMatches(entry.createdAt, dateFilter)) return false
    return !normalizedSearch || auditSearchText(entry).includes(normalizedSearch)
  })
  const successfulCount = entries.filter((entry) => entry.success).length
  const failedCount = entries.length - successfulCount
  const dryRunCount = entries.filter((entry) => entry.dryRun).length
  const hasFilters = Boolean(normalizedSearch) || statusFilter !== 'all' || executionFilter !== 'all' || contextFilter !== 'all' || resourceFilter !== 'all' || dateFilter !== 'all'

  useEffect(() => {
    if (contextFilter !== 'all' && !contextOptions.includes(contextFilter)) setContextFilter('all')
  }, [contextFilter, contextOptionKey])

  /** Clears every local history filter without changing the active cluster context. */
  function clearHistoryFilters() {
    setSearch('')
    setStatusFilter('all')
    setExecutionFilter('all')
    setContextFilter('all')
    setResourceFilter('all')
    setDateFilter('all')
  }

  if (loading && !response) return <LoadingState label="Reading local mutation history" />
  if (error && !response) return <HistoryUnavailableState onRetry={() => setRefreshNonce((value) => value + 1)} />

  return (
    <div className="history-page">
      <section className="history-intro">
        <div>
          <div className="eyebrow"><span className="eyebrow-mark" /> Local ledger / read-only</div>
          <h1>Mutation history</h1>
          <p className="intro-copy">A redacted record of reviewed and submitted Kubernetes mutations from this local KubeHaggis session. Request bodies, Secret values, credentials, and backend error text stay out of the ledger view.</p>
        </div>
        <div className="history-stamp">
          <span className="stamp-label">History source</span>
          <code>SQLite / redacted</code>
          <span className="stamp-status"><span className="signal-dot signal-dot-good" /> Read-only</span>
        </div>
      </section>

      {disconnected && <div className="history-disconnected" role="status"><span className="history-disconnected-mark"><AlertTriangle size={15} /></span><div><strong>{disconnectedContext} unavailable</strong><span>Local history remains readable. New Kubernetes mutations will stay unavailable until a context reconnects.</span></div></div>}

      <section className="history-summary-grid">
        <div className="history-summary-card"><span>Loaded records</span><strong>{entries.length}</strong><code>newest 500 from local DB</code></div>
        <div className="history-summary-card history-summary-good"><span>Accepted</span><strong>{successfulCount}</strong><code>dry-run or applied</code></div>
        <div className="history-summary-card history-summary-warning"><span>Not accepted</span><strong>{failedCount}</strong><code>safe outcome only</code></div>
        <div className="history-summary-card history-summary-neutral"><span>Dry-run reviews</span><strong>{dryRunCount}</strong><code>no cluster change</code></div>
      </section>

      <section className="history-ledger panel">
        <div className="history-ledger-heading">
          <div><span className="panel-eyebrow">Redacted event ledger</span><h2>Recent outcomes</h2></div>
          <span className={`panel-action ${error ? 'panel-action-warning' : 'panel-action-good'}`}><span />{error ? 'Showing last snapshot' : loading ? 'Refreshing' : 'Local store online'}</span>
        </div>
        <div className="history-filter-bar">
          <label className="history-search"><Search size={16} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search target, operation, context" /><kbd>/</kbd></label>
          <label className="history-filter"><span>Outcome</span><select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value as 'all' | 'success' | 'failed')}><option value="all">All outcomes</option><option value="success">Accepted</option><option value="failed">Not accepted</option></select></label>
          <label className="history-filter"><span>Mode</span><select value={executionFilter} onChange={(event) => setExecutionFilter(event.target.value as 'all' | 'dry-run' | 'applied')}><option value="all">All modes</option><option value="dry-run">Dry-run review</option><option value="applied">Applied request</option></select></label>
          <label className="history-filter"><span>Context</span><select value={contextFilter} onChange={(event) => setContextFilter(event.target.value)}><option value="all">All contexts</option>{contextOptions.map((option) => <option key={option} value={option}>{option}</option>)}</select></label>
          <label className="history-filter"><span>Resource</span><select value={resourceFilter} onChange={(event) => setResourceFilter(event.target.value)}><option value="all">All resources</option>{resourceOptions.map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></label>
          <label className="history-filter"><span>Date</span><select value={dateFilter} onChange={(event) => setDateFilter(event.target.value as 'all' | 'today' | '7d' | '30d')}><option value="all">Any time</option><option value="today">Today</option><option value="7d">Last 7 days</option><option value="30d">Last 30 days</option></select></label>
          <button className="icon-button history-refresh" onClick={() => setRefreshNonce((value) => value + 1)} disabled={loading} aria-label="Refresh mutation history" title="Refresh mutation history">{loading ? <LoaderCircle size={16} className="spin" /> : <RefreshCw size={16} />}</button>
        </div>
        {error && response && <div className="history-refresh-warning" role="status"><AlertTriangle size={14} /> Refresh failed. Showing the last safely loaded snapshot.</div>}
        {!filteredEntries.length ? <div className="history-empty"><Box size={22} /><strong>{entries.length ? 'No matching outcomes' : 'No mutation outcomes yet'}</strong><span>{entries.length ? 'Change the filters or clear the search to widen the ledger.' : 'Reviewed applies, deletes, and native operations will appear here.'}</span>{hasFilters && <button type="button" className="text-button" onClick={clearHistoryFilters}>Clear filters</button>}</div> : (
          <div className="history-table-wrap">
            <table className="history-table"><thead><tr><th>When</th><th>Outcome</th><th>Operation</th><th>Target</th><th>Context</th><th>API scope</th></tr></thead><tbody>
              {filteredEntries.map((entry) => <MutationHistoryRow key={entry.id} entry={entry} />)}
            </tbody></table>
          </div>
        )}
        <div className="history-ledger-footer"><span>Showing {filteredEntries.length} of {entries.length} loaded records</span><span><LockKeyhole size={13} /> Bodies and sensitive values are never displayed</span></div>
      </section>
    </div>
  )
}

/** Renders bounded local diagnostics for API health, discovery, watches, and native sessions. */
function DiagnosticsPage({ context, revision }: { context: string; revision: number }) {
  const [response, setResponse] = useState<DiagnosticsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [refreshNonce, setRefreshNonce] = useState(0)

  useEffect(() => {
    let active = true
    const load = () => {
      setLoading(true)
      fetchDiagnostics(context)
        .then((nextResponse) => {
          if (!active) return
          setResponse(normalizeDiagnosticsResponse(nextResponse))
          setError('')
        })
        .catch((loadError: Error) => active && setError(loadError.message || 'The local diagnostics endpoint could not be reached.'))
        .finally(() => active && setLoading(false))
    }
    load()
    const interval = window.setInterval(load, 15000)
    return () => {
      active = false
      window.clearInterval(interval)
    }
  }, [context, revision, refreshNonce])

  if (loading && !response) return <LoadingState label="Reading local diagnostics" />
  if (error && !response) return <DiagnosticsUnavailableState error={error} onRetry={() => setRefreshNonce((value) => value + 1)} />
  if (!response) return <DiagnosticsUnavailableState error="The local diagnostics endpoint returned no snapshot." onRetry={() => setRefreshNonce((value) => value + 1)} />

  const discoveryTone = diagnosticsStateTone(response.discovery.state)
  const dependencyReady = response.kubeconfig.state !== 'error' && response.context.state === 'available' && response.discovery.state === 'available' && response.audit.state === 'available'
  const sessions = response.sessions ?? { terminal: 0, logs: 0, portForward: 0, watch: 0 }
  const watches = response.watches ?? { active: 0, current: [], recent: [] }
  const requests = response.requests ?? { total: 0, errors: 0, averageDurationMilliseconds: 0, maxDurationMilliseconds: 0, statuses: {}, operations: {}, errorCategories: {} }
  const recentErrors = Array.isArray(response.recentErrors) ? [...response.recentErrors].reverse() : []
  const recentWatches = Array.isArray(watches.recent) ? [...watches.recent].reverse() : []
  const currentWatches = Array.isArray(watches.current) ? watches.current : []
  return (
    <div className="diagnostics-page">
      <section className="diagnostics-intro">
        <div>
          <div className="eyebrow"><span className="eyebrow-mark" /> Local instrument / bounded memory</div>
          <h1>Diagnostics</h1>
          <p className="intro-copy">A local-only read on the KubeHaggis process, its Kubernetes dependencies, and the native streams currently open. Bodies, credentials, review handles, Secret values, and Pod output never enter this panel.</p>
        </div>
        <div className="diagnostics-stamp">
          <span className="stamp-label">Process signal</span>
          <code>alive / {formatUptime(response.uptimeSeconds)}</code>
          <span className={`stamp-status diagnostics-status-${dependencyReady ? 'good' : 'warning'}`}><span className="signal-dot" />{dependencyReady ? 'Dependencies ready' : 'Dependencies need attention'}</span>
        </div>
      </section>

      <section className="diagnostics-signal-strip panel">
        <div className="diagnostics-signal diagnostics-signal-good"><span className="diagnostics-signal-dot" /><div><strong>Local process</strong><span>Liveness endpoint is responding</span></div><code>alive</code></div>
        <div className={`diagnostics-signal diagnostics-signal-${dependencyReady ? 'good' : 'warning'}`}><span className="diagnostics-signal-dot" /><div><strong>Dependency posture</strong><span>{dependencyReady ? 'Kubeconfig, context, discovery, and audit are available' : 'One or more local dependencies need attention'}</span></div><code>{dependencyReady ? 'ready' : 'degraded'}</code></div>
        <div className="diagnostics-signal diagnostics-signal-neutral"><span className="diagnostics-signal-dot" /><div><strong>Request correlation</strong><span>Every HTTP request returns a safe X-Request-ID</span></div><code>enabled</code></div>
      </section>

      {error && <div className="diagnostics-refresh-warning" role="status"><AlertTriangle size={14} /> Refresh failed: {error}. Showing the last safely loaded snapshot.</div>}

      <section className="diagnostics-summary-grid">
        <DiagnosticsSummaryCard label="Kubeconfig" value={`r${response.kubeconfig.revision}`} detail={`${titleCase(response.kubeconfig.state)} / ${response.kubeconfig.source || 'unknown source'}`} tone={response.kubeconfig.state === 'error' ? 'warning' : 'good'} icon={<Database size={17} />} />
        <DiagnosticsSummaryCard label="Discovery cache" value={diagnosticsStateLabel(response.discovery.state)} detail={`${response.discovery.itemCount} resources / ${diagnosticsAgeLabel(response.discovery.ageSeconds)}`} tone={discoveryTone} icon={<Box size={17} />} />
        <DiagnosticsSummaryCard label="Audit store" value={diagnosticsStateLabel(response.audit.state)} detail="SQLite mutation history" tone={response.audit.state === 'available' ? 'good' : 'warning'} icon={<History size={17} />} />
        <DiagnosticsSummaryCard label="Active sessions" value={String(diagnosticsSessionTotal(sessions))} detail={`${sessions.watch} watches / ${sessions.terminal + sessions.logs + sessions.portForward} native streams`} tone={diagnosticsSessionTotal(sessions) ? 'neutral' : 'good'} icon={<Activity size={17} />} />
      </section>

      <div className="diagnostics-grid">
        <section className="diagnostics-panel panel">
          <div className="diagnostics-panel-heading"><div><span className="panel-eyebrow">Dependency ledger</span><h2>What the local server can reach</h2></div><button className="icon-button diagnostics-refresh" onClick={() => setRefreshNonce((value) => value + 1)} disabled={loading} aria-label="Refresh diagnostics" title="Refresh diagnostics">{loading ? <LoaderCircle size={16} className="spin" /> : <RefreshCw size={16} />}</button></div>
          <div className="diagnostics-detail-list">
            <DiagnosticsDetailRow label="Kubeconfig source" value={response.kubeconfig.source || 'unknown'} note={`revision ${response.kubeconfig.revision}`} tone={response.kubeconfig.state === 'error' ? 'warning' : 'good'} />
            <DiagnosticsDetailRow label="Selected context" value={response.context.selected || 'none selected'} note={response.context.state} tone={response.context.state === 'available' ? 'good' : 'warning'} />
            <DiagnosticsDetailRow label="Discovery cache" value={diagnosticsStateLabel(response.discovery.state)} note={`${response.discovery.itemCount} resources / ${response.discovery.issueCount} issues`} tone={discoveryTone} />
            <DiagnosticsDetailRow label="Cache age" value={diagnosticsAgeLabel(response.discovery.ageSeconds)} note={response.discovery.loadedAt ? `loaded ${relativeTime(response.discovery.loadedAt)}` : 'no completed load recorded'} tone={response.discovery.state === 'available' ? 'good' : 'neutral'} />
            <DiagnosticsDetailRow label="Audit store" value={diagnosticsStateLabel(response.audit.state)} note="mutation outcomes remain redacted" tone={response.audit.state === 'available' ? 'good' : 'warning'} />
          </div>
        </section>

        <section className="diagnostics-panel panel">
          <div className="diagnostics-panel-heading"><div><span className="panel-eyebrow">Native stream board</span><h2>Sessions in flight</h2></div><span className="panel-action panel-action-neutral"><span />{diagnosticsSessionTotal(sessions)} active</span></div>
          <div className="diagnostics-session-grid">
            <DiagnosticsSessionCard icon={<TerminalSquare size={16} />} label="Terminal" count={sessions.terminal} detail="Pod exec / attach" />
            <DiagnosticsSessionCard icon={<TerminalSquare size={16} />} label="Logs" count={sessions.logs} detail="Pod log streams" />
            <DiagnosticsSessionCard icon={<Network size={16} />} label="Port-forward" count={sessions.portForward} detail="Loopback forwards" />
            <DiagnosticsSessionCard icon={<Activity size={16} />} label="Watch" count={sessions.watch} detail="SSE Kubernetes watches" />
          </div>
          <div className="diagnostics-panel-note"><LockKeyhole size={14} /> Only counts and safe context/GVR lifecycle metadata are retained.</div>
        </section>
      </div>

      <section className="diagnostics-panel diagnostics-requests panel">
        <div className="diagnostics-panel-heading"><div><span className="panel-eyebrow">HTTP instrument</span><h2>Request counters &amp; timing</h2></div><span className="panel-action panel-action-neutral"><span />{requests.total} requests</span></div>
        <div className="diagnostics-request-grid">
          <DiagnosticsRequestMetric label="Requests" value={String(requests.total)} note="bounded total" />
          <DiagnosticsRequestMetric label="Classified errors" value={String(requests.errors)} note="safe categories only" tone={requests.errors ? 'warning' : 'good'} />
          <DiagnosticsRequestMetric label="Average" value={`${formatMilliseconds(requests.averageDurationMilliseconds)} ms`} note="completed requests" />
          <DiagnosticsRequestMetric label="Slowest" value={`${formatMilliseconds(requests.maxDurationMilliseconds)} ms`} note="observed maximum" />
        </div>
        <div className="diagnostics-counter-columns">
          <DiagnosticsCounterList title="Status" values={requests.statuses} />
          <DiagnosticsCounterList title="Operation class" values={requests.operations} />
          <DiagnosticsCounterList title="Error category" values={requests.errorCategories} emptyLabel="No classified errors" />
        </div>
      </section>

      <div className="diagnostics-grid">
        <section className="diagnostics-panel panel">
          <div className="diagnostics-panel-heading"><div><span className="panel-eyebrow">Watch ledger</span><h2>Recent lifecycle</h2></div><span className="panel-action panel-action-neutral"><span />{watches.active} active</span></div>
          {!recentWatches.length ? <DiagnosticsEmptyState label="No server watch transitions yet" detail="Open a resource list or Event panel to see safe lifecycle state here." /> : <div className="diagnostics-event-list">{recentWatches.slice(0, 8).map((event, index) => <div className="diagnostics-event-row" key={`${event.at}-${event.state}-${index}`}><span className={`diagnostics-event-mark diagnostics-event-${event.errorCategory ? 'warning' : 'good'}`} /><div><strong>{diagnosticsWatchStateLabel(event.state, event.reconnect)}</strong><span>{event.resource || 'watch'}{event.context ? ` / ${event.context}` : ''}</span></div><code>{event.errorCategory || relativeTime(event.at)}</code></div>)}</div>}
          {currentWatches.length > 0 && <div className="diagnostics-watch-current">{currentWatches.slice(0, 6).map((watch, index) => <div key={`${watch.resource}-${watch.context}-${index}`}><span className={`status-pill status-${watch.active ? 'good' : watch.errorCategory ? 'warning' : 'neutral'}`}><span />{watch.active ? 'Active' : diagnosticsStateLabel(watch.state)}</span><code>{watch.resource || 'watch'}</code><small>{watch.reconnects} reconnect{watch.reconnects === 1 ? '' : 's'}</small></div>)}</div>}
        </section>

        <section className="diagnostics-panel panel">
          <div className="diagnostics-panel-heading"><div><span className="panel-eyebrow">Failure ledger</span><h2>Recent safe errors</h2></div><span className="panel-action panel-action-neutral"><span />{recentErrors.length} retained</span></div>
          {!recentErrors.length ? <DiagnosticsEmptyState label="No classified errors" detail="Backend failures appear here as categories, never as raw Kubernetes responses." /> : <div className="diagnostics-event-list">{recentErrors.slice(0, 8).map((event, index) => <div className="diagnostics-event-row" key={`${event.at}-${event.category}-${index}`}><span className="diagnostics-event-mark diagnostics-event-warning" /><div><strong>{diagnosticsStateLabel(event.category)}</strong><span>{event.operation || 'request'}{event.status ? ` / HTTP ${event.status}` : ''}</span></div><code>{relativeTime(event.at)}</code></div>)}</div>}
        </section>
      </div>

      <div className="diagnostics-footer-line"><span><RefreshCw size={13} /> Snapshot {relativeTime(response.generatedAt)}</span><span><Gauge size={13} /> Local only / no external telemetry</span><span><LockKeyhole size={13} /> Sensitive payloads excluded</span></div>
    </div>
  )
}

/** Identifies a JSON object before reading optional diagnostics fields from an older backend. */
function isDiagnosticsRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

/** Normalizes bounded numeric diagnostics maps while discarding malformed values. */
function normalizeDiagnosticsCounts(value: unknown): Record<string, number> {
  if (!isDiagnosticsRecord(value)) return {}
  return Object.fromEntries(Object.entries(value).filter(([, count]) => typeof count === 'number' && Number.isFinite(count))) as Record<string, number>
}

/** Normalizes a diagnostics payload so missing or older collections cannot break rendering. */
function normalizeDiagnosticsResponse(value: unknown): DiagnosticsResponse {
  const root = isDiagnosticsRecord(value) ? value : {}
  const kubeconfig = isDiagnosticsRecord(root.kubeconfig) ? root.kubeconfig : {}
  const contextInfo = isDiagnosticsRecord(root.context) ? root.context : {}
  const discovery = isDiagnosticsRecord(root.discovery) ? root.discovery : {}
  const watches = isDiagnosticsRecord(root.watches) ? root.watches : {}
  const sessions = isDiagnosticsRecord(root.sessions) ? root.sessions : {}
  const audit = isDiagnosticsRecord(root.audit) ? root.audit : {}
  const requests = isDiagnosticsRecord(root.requests) ? root.requests : {}
  const recentErrors: DiagnosticsErrorEvent[] = (Array.isArray(root.recentErrors) ? root.recentErrors : []).filter(isDiagnosticsRecord).map((event) => ({
    at: typeof event.at === 'string' ? event.at : '',
    status: typeof event.status === 'number' && Number.isFinite(event.status) ? event.status : undefined,
    category: typeof event.category === 'string' ? event.category : 'unknown',
    operation: typeof event.operation === 'string' ? event.operation : 'request',
  }))
  const recentWatches: DiagnosticsWatchEvent[] = (Array.isArray(watches.recent) ? watches.recent : []).filter(isDiagnosticsRecord).map((event) => ({
    at: typeof event.at === 'string' ? event.at : '',
    context: typeof event.context === 'string' ? event.context : undefined,
    resource: typeof event.resource === 'string' ? event.resource : undefined,
    state: typeof event.state === 'string' ? event.state : 'unknown',
    reconnect: event.reconnect === true,
    errorCategory: typeof event.errorCategory === 'string' ? event.errorCategory : undefined,
  }))
  const currentWatches: DiagnosticsWatchView[] = (Array.isArray(watches.current) ? watches.current : []).filter(isDiagnosticsRecord).map((watch) => ({
    context: typeof watch.context === 'string' ? watch.context : undefined,
    resource: typeof watch.resource === 'string' ? watch.resource : undefined,
    state: typeof watch.state === 'string' ? watch.state : 'unknown',
    lastAt: typeof watch.lastAt === 'string' ? watch.lastAt : '',
    errorCategory: typeof watch.errorCategory === 'string' ? watch.errorCategory : undefined,
    reconnects: typeof watch.reconnects === 'number' && Number.isFinite(watch.reconnects) ? watch.reconnects : 0,
    active: watch.active === true,
  }))
  return {
    generatedAt: typeof root.generatedAt === 'string' ? root.generatedAt : new Date(0).toISOString(),
    uptimeSeconds: typeof root.uptimeSeconds === 'number' && Number.isFinite(root.uptimeSeconds) ? root.uptimeSeconds : 0,
    kubeconfig: {
      source: typeof kubeconfig.source === 'string' ? kubeconfig.source : '',
      state: typeof kubeconfig.state === 'string' ? kubeconfig.state : 'unavailable',
      revision: typeof kubeconfig.revision === 'number' && Number.isFinite(kubeconfig.revision) ? kubeconfig.revision : 0,
    },
    context: {
      current: typeof contextInfo.current === 'string' ? contextInfo.current : '',
      selected: typeof contextInfo.selected === 'string' ? contextInfo.selected : '',
      state: typeof contextInfo.state === 'string' ? contextInfo.state : 'unavailable',
    },
    discovery: {
      context: typeof discovery.context === 'string' ? discovery.context : '',
      revision: typeof discovery.revision === 'number' && Number.isFinite(discovery.revision) ? discovery.revision : 0,
      state: typeof discovery.state === 'string' ? discovery.state : 'unavailable',
      ageSeconds: typeof discovery.ageSeconds === 'number' && Number.isFinite(discovery.ageSeconds) ? discovery.ageSeconds : undefined,
      loadedAt: typeof discovery.loadedAt === 'string' ? discovery.loadedAt : undefined,
      itemCount: typeof discovery.itemCount === 'number' && Number.isFinite(discovery.itemCount) ? discovery.itemCount : 0,
      issueCount: typeof discovery.issueCount === 'number' && Number.isFinite(discovery.issueCount) ? discovery.issueCount : 0,
    },
    watches: {
      active: typeof watches.active === 'number' && Number.isFinite(watches.active) ? watches.active : 0,
      current: currentWatches,
      recent: recentWatches,
    },
    recentErrors,
    sessions: {
      terminal: typeof sessions.terminal === 'number' && Number.isFinite(sessions.terminal) ? sessions.terminal : 0,
      logs: typeof sessions.logs === 'number' && Number.isFinite(sessions.logs) ? sessions.logs : 0,
      portForward: typeof sessions.portForward === 'number' && Number.isFinite(sessions.portForward) ? sessions.portForward : 0,
      watch: typeof sessions.watch === 'number' && Number.isFinite(sessions.watch) ? sessions.watch : 0,
    },
    audit: { state: typeof audit.state === 'string' ? audit.state : 'unavailable' },
    requests: {
      total: typeof requests.total === 'number' && Number.isFinite(requests.total) ? requests.total : 0,
      errors: typeof requests.errors === 'number' && Number.isFinite(requests.errors) ? requests.errors : 0,
      averageDurationMilliseconds: typeof requests.averageDurationMilliseconds === 'number' && Number.isFinite(requests.averageDurationMilliseconds) ? requests.averageDurationMilliseconds : 0,
      maxDurationMilliseconds: typeof requests.maxDurationMilliseconds === 'number' && Number.isFinite(requests.maxDurationMilliseconds) ? requests.maxDurationMilliseconds : 0,
      statuses: normalizeDiagnosticsCounts(requests.statuses),
      operations: normalizeDiagnosticsCounts(requests.operations),
      errorCategories: normalizeDiagnosticsCounts(requests.errorCategories),
    },
  }
}

/** Renders the explicit disconnected state for a local diagnostics endpoint. */
function DiagnosticsUnavailableState({ error, onRetry }: { error: string; onRetry: () => void }) {
  return <div className="state-page"><div className="state-card"><span className="state-icon state-icon-warning"><Gauge size={22} /></span><span className="eyebrow"><span className="eyebrow-mark" /> Local instrument disconnected</span><h1>Diagnostics are offline.</h1><p>The local diagnostics snapshot could not be read. No dependency details are guessed while the local API is disconnected.</p><div className="error-block"><code>{error}</code></div><button className="secondary-button" onClick={onRetry}><RefreshCw size={15} /> Try again</button></div></div>
}

/** Renders one large diagnostics summary card. */
function DiagnosticsSummaryCard({ label, value, detail, tone, icon }: { label: string; value: string; detail: string; tone: 'good' | 'warning' | 'neutral'; icon: ReactNode }) {
  return <article className={`diagnostics-summary-card diagnostics-summary-${tone}`}><div className="diagnostics-summary-top"><span>{icon}</span><span>{label}</span></div><strong>{value}</strong><code>{detail}</code></article>
}

/** Renders one safe dependency row with state tone and bounded notes. */
function DiagnosticsDetailRow({ label, value, note, tone }: { label: string; value: string; note: string; tone: 'good' | 'warning' | 'neutral' }) {
  return <div className="diagnostics-detail-row"><div><strong>{label}</strong><span>{note}</span></div><span className={`diagnostics-detail-value diagnostics-detail-${tone}`}>{value}</span></div>
}

/** Renders one active native-session count. */
function DiagnosticsSessionCard({ icon, label, count, detail }: { icon: ReactNode; label: string; count: number; detail: string }) {
  return <div className="diagnostics-session-card"><span>{icon}</span><div><strong>{label}</strong><small>{detail}</small></div><b>{count}</b></div>
}

/** Renders one request timing metric. */
function DiagnosticsRequestMetric({ label, value, note, tone = 'neutral' }: { label: string; value: string; note: string; tone?: 'good' | 'warning' | 'neutral' }) {
  return <div className={`diagnostics-request-metric diagnostics-request-${tone}`}><span>{label}</span><strong>{value}</strong><code>{note}</code></div>
}

/** Renders a deterministic bounded counter map as compact rows. */
function DiagnosticsCounterList({ title, values, emptyLabel = 'No observations' }: { title: string; values: Record<string, number>; emptyLabel?: string }) {
  const entries = Object.entries(values).sort((left, right) => right[1] - left[1] || left[0].localeCompare(right[0])).slice(0, 8)
  return <div className="diagnostics-counter-list"><span className="panel-eyebrow">{title}</span>{entries.length ? entries.map(([key, value]) => <div key={key}><code>{key || 'unknown'}</code><strong>{value}</strong></div>) : <small>{emptyLabel}</small>}</div>
}

/** Renders an explicit empty state inside a diagnostics panel. */
function DiagnosticsEmptyState({ label, detail }: { label: string; detail: string }) {
  return <div className="diagnostics-empty"><Check size={17} /><strong>{label}</strong><span>{detail}</span></div>
}

/** Returns the visual tone for one safe diagnostics state. */
function diagnosticsStateTone(state: string): 'good' | 'warning' | 'neutral' {
  if (state === 'available' || state === 'ready' || state === 'active') return 'good'
  if (state === 'partial' || state === 'error' || state === 'unavailable' || state === 'permission-denied' || state === 'degraded') return 'warning'
  return 'neutral'
}

/** Converts a diagnostics state into a compact operator label. */
function diagnosticsStateLabel(state: string): string {
  const labels: Record<string, string> = {
    available: 'Available',
    active: 'Active',
    changed: 'Changed',
    degraded: 'Degraded',
    error: 'Error',
    loading: 'Loading',
    missing: 'Missing',
    not_checked: 'Not checked',
    'not-checked': 'Not checked',
    'permission-denied': 'RBAC denied',
    unavailable: 'Unavailable',
  }
  return labels[state] || titleCase(state || 'Unknown')
}

/** Formats a bounded discovery-cache age without inventing freshness. */
function diagnosticsAgeLabel(seconds?: number): string {
  if (seconds === undefined) return 'not loaded'
  if (seconds < 60) return `${seconds}s old`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m old`
  return `${Math.floor(seconds / 3600)}h old`
}

/** Formats process uptime using compact field-console time units. */
function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  return `${Math.floor(seconds / 3600)}h ${Math.floor(seconds / 60) % 60}m`
}

/** Formats a request duration without noisy decimal precision. */
function formatMilliseconds(value: number): string {
  if (!Number.isFinite(value)) return '0'
  if (value < 10) return value.toFixed(1)
  return Math.round(value).toString()
}

/** Returns the total count of active diagnostics sessions. */
function diagnosticsSessionTotal(sessions: DiagnosticsResponse['sessions']): number {
  return sessions.terminal + sessions.logs + sessions.portForward + sessions.watch
}

/** Converts a watch lifecycle event into safe operator-facing copy. */
function diagnosticsWatchStateLabel(state: string, reconnect = false): string {
  if (state === 'opened') return reconnect ? 'Watch reconnected' : 'Watch opened'
  if (state === 'error') return 'Watch error classified'
  if (state === 'closed') return 'Watch closed'
  return diagnosticsStateLabel(state)
}

/** Adds only safe correlation and retry metadata to a native stream failure message. */
function streamFailureMessage(message: { message?: string; requestId?: string; retry?: RetryClassification }, fallback: string): string {
  const details = [message.requestId ? `request ID ${message.requestId}` : '', message.retry ? `retry: ${message.retry}` : ''].filter(Boolean).join('; ')
  const text = message.message || fallback
  return details ? `${text} (${details})` : text
}

/** Renders the explicit disconnected state for an unavailable local audit store. */
function HistoryUnavailableState({ onRetry }: { onRetry: () => void }) {
  return <div className="state-page"><div className="state-card"><span className="state-icon state-icon-warning"><Database size={22} /></span><span className="eyebrow"><span className="eyebrow-mark" /> Local ledger disconnected</span><h1>Mutation history is offline.</h1><p>The local SQLite audit store could not be read. No operation details are shown while the history source is disconnected.</p><button className="secondary-button" onClick={onRetry}><RefreshCw size={15} /> Try again</button></div></div>
}

/** Renders one safe, dense row in the local mutation ledger. */
function MutationHistoryRow({ entry }: { entry: AuditEntry }) {
  return <tr><td><time className="history-time" dateTime={entry.createdAt} title={formatAuditTimestamp(entry.createdAt)}><strong>{relativeTime(entry.createdAt)}</strong><span>{formatAuditTimestamp(entry.createdAt)}</span></time></td><td><span className={`status-pill status-${auditOutcomeTone(entry)}`}><span />{auditOutcomeLabel(entry)}</span><small className="history-outcome-note">{auditOutcomeNote(entry)}</small></td><td><strong className="history-operation">{titleCase(entry.operation || 'mutation')}</strong><code>{entry.dryRun ? 'server dry-run' : 'write request'}</code></td><td><strong className="history-target">{auditTargetLabel(entry)}</strong><code>{auditResourceLabel(entry)}</code></td><td><code className="history-context">{entry.context || 'unknown context'}</code></td><td><span className="history-scope-mode">{entry.dryRun ? 'review' : 'mutation'}</span><code>{entry.group ? `${entry.group}/${entry.version}` : entry.version}</code></td></tr>
}

/** Returns a stable resource key used by the history resource filter. */
function auditResourceKey(entry: AuditEntry): string {
  return `${entry.group}\u0000${entry.version}\u0000${entry.resource}`
}

/** Formats one audit resource as a safe API-version and resource label. */
function auditResourceLabel(entry: AuditEntry): string {
  const apiVersion = entry.group ? `${entry.group}/${entry.version}` : entry.version
  return `${apiVersion || 'unknown API'} / ${titleCase(entry.resource || 'resource')}`
}

/** Formats a redacted audit target without exposing request content. */
function auditTargetLabel(entry: AuditEntry): string {
  const target = entry.name || titleCase(entry.resource || 'resource')
  return entry.namespace ? `${entry.namespace}/${target}` : target
}

/** Builds searchable text from safe audit coordinates only. */
function auditSearchText(entry: AuditEntry): string {
  return [entry.context, entry.operation, entry.group, entry.version, entry.resource, entry.namespace, entry.name].filter(Boolean).join(' ').toLowerCase()
}

/** Applies a bounded date-window filter to an audit timestamp. */
function auditDateMatches(value: string, filter: 'all' | 'today' | '7d' | '30d'): boolean {
  if (filter === 'all') return true
  const timestamp = new Date(value).getTime()
  if (!Number.isFinite(timestamp)) return false
  const now = new Date()
  if (filter === 'today') {
    now.setHours(0, 0, 0, 0)
    return timestamp >= now.getTime()
  }
  const days = filter === '7d' ? 7 : 30
  return timestamp >= Date.now() - days * 24 * 60 * 60 * 1000
}

/** Formats an audit timestamp for the operator's local timezone. */
function formatAuditTimestamp(value: string): string {
  const timestamp = new Date(value)
  if (!Number.isFinite(timestamp.getTime())) return 'Unknown time'
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(timestamp)
}

/** Returns the visual tone for one redacted mutation outcome. */
function auditOutcomeTone(entry: AuditEntry): 'good' | 'neutral' | 'warning' {
  if (!entry.success) return 'warning'
  return entry.dryRun ? 'neutral' : 'good'
}

/** Returns the safe short outcome label shown in the history table. */
function auditOutcomeLabel(entry: AuditEntry): string {
  if (!entry.success) return 'Not accepted'
  return entry.dryRun ? 'Dry-run passed' : 'Applied'
}

/** Returns a safe explanation that never includes backend error text. */
function auditOutcomeNote(entry: AuditEntry): string {
  if (!entry.success) return 'No success recorded'
  return entry.dryRun ? 'No cluster change' : 'Mutation accepted'
}

/** Returns the compact label for one watched-panel freshness state. */
function freshnessLabel(state: FreshnessState): string {
  switch (state) {
    case 'live':
      return 'Live'
    case 'reconnecting':
      return 'Reconnecting'
    case 'stale':
      return 'Stale data'
    case 'refreshing':
      return 'Refreshing'
    default:
      return 'Unavailable'
  }
}

/** Formats a UI-only successful refresh timestamp without implying Kubernetes time. */
function formatRefreshTimestamp(timestamp: number | null): string {
  if (timestamp === null || !Number.isFinite(timestamp)) return 'No successful refresh'
  return `Last successful refresh ${new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(timestamp))}`
}

/** Returns the explicit explanation shown while a watched panel is not live. */
function freshnessMessage(snapshot: WatchSnapshot): string {
  switch (snapshot.state) {
    case 'reconnecting':
      return 'The watch closed or lost its connection. A fresh relist will run before the next watch.'
    case 'stale':
      return snapshot.hasData ? 'Showing the last successful snapshot; refresh attempts are still in progress.' : 'The latest refresh failed; retrying the Kubernetes read.'
    case 'refreshing':
      return snapshot.hasData ? 'Refreshing from Kubernetes; the last successful snapshot remains visible.' : 'Reading the first Kubernetes snapshot.'
    default:
      return snapshot.hasData ? 'The live source is unavailable; the last successful snapshot is marked stale.' : 'No successful Kubernetes snapshot is available yet.'
  }
}

/** Renders the state and last-success metadata for a watched panel. */
function FreshnessStatus({ snapshot }: { snapshot: WatchSnapshot }) {
  return <span className="freshness-status"><span className={`watch-state-chip watch-state-${snapshot.state}`}><span />{freshnessLabel(snapshot.state)}</span><span className="watch-last-refresh">{formatRefreshTimestamp(snapshot.lastSuccessfulRefresh)}</span></span>
}

/** Renders an explicit non-live notice so stale data cannot look like a live panel. */
function FreshnessNotice({ snapshot }: { snapshot: WatchSnapshot }) {
  if (snapshot.state === 'live') return null
  const icon = snapshot.state === 'refreshing' ? <LoaderCircle size={14} className="spin" /> : <AlertTriangle size={14} />
  return <div className={`freshness-notice freshness-notice-${snapshot.state}`} role="status"><span className="freshness-notice-icon">{icon}</span><div><strong>{freshnessLabel(snapshot.state)}</strong><span>{freshnessMessage(snapshot)}</span></div></div>
}

/** Returns the compact footer copy used when a watch is not live. */
function freshnessFooterLabel(state: FreshnessState): string {
  switch (state) {
    case 'refreshing':
      return 'refreshing from Kubernetes'
    case 'reconnecting':
      return 'reconnecting after a fresh relist'
    case 'stale':
      return 'last successful data is marked stale'
    default:
      return 'live data is unavailable'
  }
}

/** Renders the generic live-updating resource table. */
function ResourceListPage({ context, namespace, definition, revision, reloadEpoch, writeEnabled, onOpen, onCreate }: { context: string; namespace: string; definition: ResourceDefinition; revision: number; reloadEpoch: number; writeEnabled: boolean; onOpen: (name: string, namespace?: string) => void; onCreate: () => void }) {
  const [response, setResponse] = useState<ResourceListResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [labelSelector, setLabelSelector] = useState('')
  const [statusFilter, setStatusFilter] = useState('all')
  const [sortKey, setSortKey] = useState<ResourceSortKey>('name')
  const [sortDirection, setSortDirection] = useState<ResourceSortDirection>('asc')
  const [visibleColumns, setVisibleColumns] = useState<ResourceColumnKey[]>(defaultResourceColumns)
  const [columnsOpen, setColumnsOpen] = useState(false)
  const [pageSize, setPageSize] = useState(100)
  const [pagination, setPagination] = useState<ResourcePaginationState>({ queryKey: '', pageIndex: 0, tokens: [''] })
  const [watchSnapshot, setWatchSnapshot] = useState<WatchSnapshot>(() => initialWatchSnapshot())
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [selectedKeys, setSelectedKeys] = useState<string[]>([])
  const [bulkReview, setBulkReview] = useState<BulkDeleteResponse | null>(null)
  const [bulkReviewToken, setBulkReviewToken] = useState<string | undefined>()
  const [bulkReviewSelection, setBulkReviewSelection] = useState<string[]>([])
  const [bulkResults, setBulkResults] = useState<BulkDeleteResponse | null>(null)
  const [bulkConfirmed, setBulkConfirmed] = useState(false)
  const [bulkConfirmation, setBulkConfirmation] = useState('')
  const [bulkBusy, setBulkBusy] = useState<'review' | 'apply' | ''>('')
  const [bulkError, setBulkError] = useState('')
  const listQueryKey = resourceListQueryKey(context, definition, namespace, labelSelector, pageSize)
  const paginationMatchesQuery = pagination.queryKey === listQueryKey
  const pageIndex = paginationMatchesQuery ? pagination.pageIndex : 0
  const continueToken = paginationMatchesQuery ? pagination.tokens[pageIndex] || '' : ''

  useEffect(() => {
    setPagination({ queryKey: listQueryKey, pageIndex: 0, tokens: [''] })
    setResponse(null)
    setError('')
    setSelectedKeys([])
    setBulkReview(null)
    setBulkReviewToken(undefined)
    setBulkReviewSelection([])
    setBulkConfirmed(false)
    setBulkConfirmation('')
    setBulkResults(null)
    setBulkError('')
  }, [listQueryKey, revision, reloadEpoch])

  useEffect(() => {
    if (!writeEnabled) {
      setBulkReview(null)
      setBulkReviewToken(undefined)
      setBulkReviewSelection([])
      setBulkConfirmed(false)
      setBulkConfirmation('')
    }
  }, [writeEnabled])

  useEffect(() => {
    setWatchSnapshot(initialWatchSnapshot())
    setError('')
    const controller = new ReliableWatchController<ResourceListResponse>({
      load: (signal) => fetchResources(context, definition, namespace, labelSelector, { limit: pageSize, continueToken }, signal),
      getResourceVersion: (data) => data.resourceVersion,
      watchUrl: (resourceVersion) => watchUrl(context, definition, namespace, resourceVersion),
       onData: (data) => {
         setResponse(data)
         setBulkReview(null)
         setBulkReviewToken(undefined)
         setBulkReviewSelection([])
         setBulkConfirmed(false)
         setBulkConfirmation('')
         setSelectedKeys((current) => current.filter((key) => data.items.some((item) => resourceItemKey(item) === key)))
        setPagination((current) => {
          if (current.queryKey !== listQueryKey || current.pageIndex !== pageIndex) return current
          const tokens = current.tokens.slice(0, pageIndex + 1)
          if (data.continue) tokens.push(data.continue)
          return { ...current, tokens }
        })
        setError('')
        setLoading(false)
      },
      onState: (snapshot) => {
        setWatchSnapshot(snapshot)
        setLoading(snapshot.state === 'refreshing')
        if (snapshot.state === 'stale' || snapshot.state === 'unavailable') setError(snapshot.message || '')
      },
    })
    controller.start()
    return () => controller.stop()
  }, [context, definition, namespace, labelSelector, pageSize, continueToken, pageIndex, listQueryKey, revision, reloadEpoch, refreshNonce])

  if (!context) return <SetupState />
  const statusOptions = Array.from(new Set(response?.items.map(resourceStatusLabel) ?? [])).sort((left, right) => left.localeCompare(right))
  if (statusFilter !== 'all' && !statusOptions.includes(statusFilter)) statusOptions.unshift(statusFilter)
  const filtered = sortResourceItems(
    response?.items.filter((item) => (typeof item.name === 'string' ? item.name : '').toLowerCase().includes(search.trim().toLowerCase()) && resourceStatusMatches(item, statusFilter)) ?? [],
    sortKey,
    sortDirection,
  )
  const selected = response?.items.filter((item) => selectedKeys.includes(resourceItemKey(item))) ?? []
  const allFilteredSelected = filtered.length > 0 && filtered.every((item) => selectedKeys.includes(resourceItemKey(item)))
  const selectionLocked = Boolean(bulkReview) || Boolean(bulkBusy)
  const visibleColumnDefinitions = resourceColumnDefinitions.filter((column) => visibleColumns.includes(column.key))
  const printerColumns = definition.customResourceCapabilities?.printerColumns ?? []
  const tableColumnCount = visibleColumnDefinitions.length + printerColumns.length + 2

  /** Clears page-local selection and starts the first continuation page when needed. */
  function resetLocalView() {
    setSelectedKeys([])
    clearBulkReview()
    setBulkResults(null)
    setError('')
    if (pageIndex === 0) return
    setResponse(null)
    setPagination({ queryKey: listQueryKey, pageIndex: 0, tokens: [''] })
  }

  /** Changes a local status filter without presenting it as a Kubernetes query. */
  function changeStatusFilter(value: string) {
    setStatusFilter(value)
    resetLocalView()
  }

  /** Changes the local sort key and returns to the first visible page. */
  function changeSortKey(value: ResourceSortKey) {
    setSortKey(value)
    resetLocalView()
  }

  /** Toggles the direction of the local page sort. */
  function toggleSortDirection() {
    setSortDirection((current) => current === 'asc' ? 'desc' : 'asc')
    resetLocalView()
  }

  /** Changes one optional summary column while keeping Name permanently visible. */
  function toggleColumn(column: ResourceColumnDefinition, checked: boolean) {
    if (column.required) return
    setVisibleColumns((current) => {
      const next = new Set(current)
      if (checked) next.add(column.key)
      else next.delete(column.key)
      return resourceColumnDefinitions.filter((candidate) => next.has(candidate.key)).map((candidate) => candidate.key)
    })
  }

  /** Advances to the next page using only the opaque token returned by Kubernetes. */
  function nextPage() {
    if (!response?.continue || loading) return
    const token = response.continue
    setSelectedKeys([])
    clearBulkReview()
    setBulkResults(null)
    setResponse(null)
    setPagination((current) => {
      const currentIndex = current.queryKey === listQueryKey ? current.pageIndex : 0
      const tokens = current.queryKey === listQueryKey ? current.tokens.slice(0, currentIndex + 1) : ['']
      tokens.push(token)
      return { queryKey: listQueryKey, pageIndex: currentIndex + 1, tokens }
    })
  }

  /** Returns to a previously fetched continuation page without guessing a token. */
  function previousPage() {
    if (pageIndex === 0 || loading) return
    setSelectedKeys([])
    clearBulkReview()
    setBulkResults(null)
    setResponse(null)
    setPagination((current) => ({
      queryKey: listQueryKey,
      pageIndex: Math.max(0, current.queryKey === listQueryKey ? current.pageIndex - 1 : 0),
      tokens: current.queryKey === listQueryKey ? current.tokens : [''],
    }))
  }

  /** Clears a bulk review and any stale result when the selection changes. */
  function clearBulkReview() {
    setBulkReview(null)
    setBulkReviewToken(undefined)
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
      const response = await deleteResources(context, definition, namespace, selected.map(resourceDeleteItem), true, false, '')
      if (!response.reviewToken) {
        setBulkError('The server did not return a mutation review handle. Review the delete again.')
        return
      }
      setBulkReview(response.value)
      setBulkReviewToken(response.reviewToken)
      setBulkReviewSelection(selected.map(resourceDeleteReviewKey).sort())
      setBulkConfirmed(false)
      setBulkConfirmation('')
    } catch (reviewError) {
      setBulkReview(null)
      setBulkReviewToken(undefined)
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
    if (!bulkReview || bulkReview.status !== 'dry-run passed' || !bulkReviewToken) {
      setBulkError('Resolve every dry-run error before applying this bulk delete.')
      return
    }
    const currentSelection = selected.map(resourceDeleteReviewKey).sort()
    if (currentSelection.length !== bulkReviewSelection.length || currentSelection.some((key, index) => key !== bulkReviewSelection[index])) {
      setBulkReview(null)
      setBulkReviewToken(undefined)
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
      const response = await deleteResources(context, definition, namespace, selected.map(resourceDeleteItem), false, true, bulkConfirmation.trim(), bulkReviewToken)
      setBulkResults(response.value)
      setBulkReview(null)
      setBulkReviewToken(undefined)
      setBulkReviewSelection([])
      setBulkConfirmed(false)
      setBulkConfirmation('')
      setSelectedKeys((current) => current.filter((key) => !response.value.items.some((item) => item.success && resourceItemKey(item) === key)))
      setRefreshNonce((value) => value + 1)
    } catch (applyError) {
      setBulkReview(null)
      setBulkReviewToken(undefined)
      setBulkConfirmed(false)
      setBulkConfirmation('')
      setBulkError((applyError as Error).message)
    } finally {
      setBulkBusy('')
    }
  }

  return (
    <div className="resource-page">
      <section className="resource-intro">
        <div><div className="eyebrow"><span className="eyebrow-mark" /> Resource collection</div><h1>{titleCase(definition.resource)}</h1><p>{definition.namespaced ? 'Namespaced resource' : 'Cluster-scoped resource'} / {namespace === 'all' || !definition.namespaced ? 'all namespaces' : namespace}</p></div>
        <div className="resource-intro-meta"><FreshnessStatus snapshot={watchSnapshot} /><span>Page {pageIndex + 1} / {response?.items.length ?? 0} objects</span></div>
      </section>
      <div className="table-toolbar">
        <label className="search-field"><Search size={16} /><input value={search} onChange={(event) => { setSearch(event.target.value); resetLocalView() }} placeholder={`Search ${definition.resource}`} /><kbd>/</kbd></label>
        <label className="filter-field"><ListFilter size={15} /><input value={labelSelector} onChange={(event) => setLabelSelector(event.target.value)} placeholder="label selector" /></label>
        <label className="resource-control"><span>Status <em>local page</em></span><select value={statusFilter} onChange={(event) => changeStatusFilter(event.target.value)}><option value="all">All statuses</option>{statusOptions.map((status) => <option value={status} key={status}>{status}</option>)}</select></label>
        <label className="resource-control"><span>Sort <em>local page</em></span><select value={sortKey} onChange={(event) => changeSortKey(event.target.value as ResourceSortKey)}><option value="name">Name</option><option value="namespace">Namespace</option><option value="status">Status</option><option value="ready">Ready</option><option value="age">Age</option></select></label>
        <button type="button" className="icon-button sort-direction" onClick={toggleSortDirection} aria-label={`Sort ${sortDirection === 'asc' ? 'descending' : 'ascending'}`} title={`Sort ${sortDirection === 'asc' ? 'descending' : 'ascending'}`}>{sortDirection === 'asc' ? <ArrowUp size={15} /> : <ArrowDown size={15} />}</button>
        <div className="column-menu">
          <button type="button" className="secondary-button column-button" onClick={() => setColumnsOpen((open) => !open)} aria-expanded={columnsOpen}><Settings2 size={14} /> Columns <span>{visibleColumnDefinitions.length}/{resourceColumnDefinitions.length}</span></button>
          {columnsOpen && <div className="column-menu-panel"><strong>Visible columns</strong>{resourceColumnDefinitions.map((column) => <label key={column.key}><input type="checkbox" checked={visibleColumns.includes(column.key)} disabled={column.required} onChange={(event) => toggleColumn(column, event.target.checked)} /><span>{column.label}</span>{column.required && <em>required</em>}</label>)}<small>Columns change this browser view only.</small></div>}
        </div>
        <button className="icon-button toolbar-action" onClick={() => setRefreshNonce((value) => value + 1)} aria-label="Refresh resources"><RefreshCw size={16} /></button>
        <button className="primary-button" disabled={!writeEnabled} onClick={onCreate} title={writeEnabled ? `Create a ${definition.kind}` : 'Enable write mode to create resources'}><span>+</span> New {definition.kind}</button>
      </div>
      <FreshnessNotice snapshot={watchSnapshot} />
      <div className="table-view-note"><span>Local page view</span> Search, status, and sorting inspect only the loaded Kubernetes page. Label selector is sent to Kubernetes; columns stay in this browser.{printerColumns.length ? ` Safe CRD columns: ${printerColumns.map((column) => column.name).join(', ')}.` : ''}{definition.customResourceCapabilities?.printerColumnWarnings?.length ? ' Unsupported CRD columns use the generic table fallback.' : ''}</div>
      <div className="selection-toolbar">
        <span><strong>{selected.length}</strong> selected / {definition.kind} / {namespace === 'all' || !definition.namespaced ? 'cluster scope' : namespace}</span>
        <div><button type="button" className="secondary-button danger-button" disabled={!writeEnabled || !selected.length || selectionLocked} onClick={() => void reviewBulkDelete()}><Trash2 size={14} /> Review delete{selected.length ? ` (${selected.length})` : ''}</button>{selected.length > 0 && <button type="button" className="text-button" disabled={selectionLocked} onClick={() => { setSelectedKeys([]); clearBulkReview(); setBulkResults(null) }}>Clear selection</button>}</div>
      </div>
      {error ? <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div> : null}
      {bulkError ? <div className="inline-error page-error"><AlertTriangle size={15} />{bulkError}</div> : null}
      {bulkReview && <BulkDeleteReviewPanel definition={definition} namespace={namespace} items={selected} review={bulkReview} confirmed={bulkConfirmed} confirmation={bulkConfirmation} busy={bulkBusy} onConfirmed={setBulkConfirmed} onConfirmation={setBulkConfirmation} onCancel={clearBulkReview} onApply={() => void applyReviewedBulkDelete()} />}
      {bulkResults && <BulkDeleteResults definition={definition} result={bulkResults} />}
      <div className="resource-table-wrap">
        <table className="resource-table"><thead><tr><th><input className="table-checkbox" type="checkbox" checked={allFilteredSelected} disabled={!filtered.length || selectionLocked} aria-label="Select all visible resources" onChange={(event) => toggleFilteredSelection(event.target.checked)} /></th>{visibleColumnDefinitions.map((column) => <th key={column.key}>{column.label}</th>)}{printerColumns.map((column) => <th key={`printer-${column.name}`} title={column.description}>{column.name}</th>)}<th aria-label="Actions" /></tr></thead><tbody>
          {loading && !response ? <tr><td colSpan={tableColumnCount}><div className="table-loading"><LoaderCircle size={18} className="spin" /> Reading API objects</div></td></tr> : null}
           {!loading && !response ? <tr><td colSpan={tableColumnCount}><div className="table-empty"><AlertTriangle size={20} /><strong>Resource data unavailable</strong><span>Waiting for a successful Kubernetes refresh.</span></div></td></tr> : null}
           {!loading && response && !filtered.length ? <tr><td colSpan={tableColumnCount}><div className="table-empty"><Box size={20} /><strong>No matching objects</strong><span>Try a different local filter or label selector.</span></div></td></tr> : null}
          {filtered.map((item) => <ResourceRow key={resourceItemKey(item)} item={item} columns={visibleColumnDefinitions.map((column) => column.key)} printerColumns={printerColumns} selected={selectedKeys.includes(resourceItemKey(item))} selectionDisabled={selectionLocked} onToggle={toggleSelection} onOpen={onOpen} />)}
        </tbody></table>
      </div>
       <div className="table-footer resource-pagination"><span>Showing {filtered.length} of {response?.items.length ?? 0} on page {pageIndex + 1}{response?.continue ? ' / more available' : ''}</span><div className="pagination-controls"><label>Rows <select value={pageSize} onChange={(event) => setPageSize(Number(event.target.value))}>{resourcePageSizes.map((size) => <option value={size} key={size}>{size}</option>)}</select></label><button type="button" className="secondary-button" onClick={previousPage} disabled={pageIndex === 0 || loading}><ChevronLeft size={14} /> Previous</button><button type="button" className="secondary-button" onClick={nextPage} disabled={!response?.continue || loading}>Next <ChevronRight size={14} /></button></div><span><span className="table-footer-dot" /> {watchSnapshot.state === 'live' ? 'updates arrive through the Kubernetes watch' : freshnessFooterLabel(watchSnapshot.state)}</span></div>
    </div>
  )
}

/** Renders one row in the generic resource table. */
function ResourceRow({ item, columns, printerColumns, selected, selectionDisabled, onToggle, onOpen }: { item: ResourceSummary; columns: ResourceColumnKey[]; printerColumns: CustomResourcePrinterColumn[]; selected: boolean; selectionDisabled: boolean; onToggle: (item: ResourceSummary, checked: boolean) => void; onOpen: (name: string, namespace?: string) => void }) {
  return <tr className={selected ? 'resource-row-selected' : ''} onClick={() => onOpen(item.name, item.namespace)}><td><input className="table-checkbox" type="checkbox" checked={selected} disabled={selectionDisabled} aria-label={`Select ${item.name}`} onClick={(event) => event.stopPropagation()} onChange={(event) => onToggle(item, event.target.checked)} /></td>{columns.map((column) => <td key={column}>{renderResourceCell(column, item, onOpen)}</td>)}{printerColumns?.map((column) => <td key={`printer-${column.name}`}><code className="printer-column-cell">{printerColumnDisplay(item, column.name)}</code></td>)}<td><button className="icon-button row-menu" aria-label={`Actions for ${item.name}`} onClick={(event) => event.stopPropagation()}><MoreHorizontal size={16} /></button></td></tr>
}

/** Builds the server query identity used to invalidate stale continuation tokens. */
function resourceListQueryKey(context: string, definition: ResourceDefinition, namespace: string, labelSelector: string, pageSize: number): string {
  return [context, definition.group, definition.version, definition.resource, definition.namespaced ? 'namespaced' : 'cluster', namespace, labelSelector, pageSize].join('\u001f')
}

/** Returns a safe display value for generic status data, including incomplete CRD summaries. */
function resourceStatusLabel(item: ResourceSummary): string {
  const status = typeof item.status === 'string' ? item.status.trim() : ''
  return status || 'Unknown'
}

/** Matches a local status filter without assuming a fixed set of Kubernetes status values. */
function resourceStatusMatches(item: ResourceSummary, filter: string): boolean {
  return filter === 'all' || resourceStatusLabel(item).toLowerCase() === filter.toLowerCase()
}

/** Returns the visual tone for a generic status while preserving unknown values as neutral. */
function resourceStatusTone(status: string): 'good' | 'neutral' | 'warning' {
  const normalized = status.toLowerCase()
  if (['true', 'running', 'active', 'succeeded', 'bound', 'ready'].includes(normalized)) return 'good'
  if (['false', 'failed', 'lost'].includes(normalized)) return 'warning'
  return 'neutral'
}

/** Returns the text used when locally sorting a generic summary field. */
function resourceSortText(item: ResourceSummary, key: ResourceSortKey): string {
  switch (key) {
    case 'namespace':
      return item.namespace || ''
    case 'status':
      return resourceStatusLabel(item)
    case 'ready':
      return item.ready || ''
    case 'age':
      return item.createdAt || ''
    default:
      return item.name || ''
  }
}

/** Compares two generic summaries using only fields guaranteed by the list response. */
function compareResourceItems(left: ResourceSummary, right: ResourceSummary, key: ResourceSortKey): number {
  if (key === 'age') {
    const leftTime = Date.parse(left.createdAt)
    const rightTime = Date.parse(right.createdAt)
    const leftKnown = Number.isFinite(leftTime)
    const rightKnown = Number.isFinite(rightTime)
    if (!leftKnown && rightKnown) return 1
    if (leftKnown && !rightKnown) return -1
    if (leftKnown && rightKnown && leftTime !== rightTime) return leftTime - rightTime
  } else {
    const comparison = resourceSortText(left, key).localeCompare(resourceSortText(right, key), undefined, { numeric: true, sensitivity: 'base' })
    if (comparison !== 0) return comparison
  }
  const nameComparison = (typeof left.name === 'string' ? left.name : '').localeCompare(typeof right.name === 'string' ? right.name : '', undefined, { numeric: true, sensitivity: 'base' })
  if (nameComparison !== 0) return nameComparison
  return (left.namespace || '').localeCompare(right.namespace || '', undefined, { numeric: true, sensitivity: 'base' })
}

/** Sorts only the currently loaded Kubernetes page and never claims server-side ordering. */
function sortResourceItems(items: ResourceSummary[], key: ResourceSortKey, direction: ResourceSortDirection): ResourceSummary[] {
  return [...items].sort((left, right) => {
    const comparison = compareResourceItems(left, right, key)
    return direction === 'asc' ? comparison : -comparison
  })
}

/** Renders one configurable generic summary cell with safe fallbacks for missing fields. */
function renderResourceCell(column: ResourceColumnKey, item: ResourceSummary, onOpen: (name: string, namespace?: string) => void): ReactNode {
  switch (column) {
    case 'name':
      return <><button className="resource-name" onClick={(event) => { event.stopPropagation(); onOpen(item.name, item.namespace) }}>{item.name || 'Unnamed'}</button><span className="resource-uid">{typeof item.uid === 'string' && item.uid ? item.uid.slice(0, 12) : 'no uid'}</span></>
    case 'namespace':
      return <span className="namespace-cell">{item.namespace || 'cluster'}</span>
    case 'status': {
      const status = resourceStatusLabel(item)
      return <span className={`status-pill status-${resourceStatusTone(status)}`}><span />{status}</span>
    }
    case 'ready':
      return <code className="ready-cell">{item.ready || '—'}</code>
    case 'age':
      return <span className="age-cell">{relativeTime(item.createdAt)}</span>
    case 'labels': {
      const entries = Object.entries(item.labels ?? {}).slice(0, 2)
      const labelCount = Object.keys(item.labels ?? {}).length
      return <div className="label-stack">{entries.map(([key, value]) => <span key={key}>{key}={value}</span>)}{labelCount > 2 && <span>+{labelCount - 2}</span>}{labelCount === 0 && <span>—</span>}</div>
    }
    case 'resourceVersion':
      return <code className="ready-cell">{item.resourceVersion ? `rv/${item.resourceVersion}` : '—'}</code>
  }
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
function ResourceDetailPage({ context, namespace, definition, name, revision, reloadEpoch, writeEnabled, onBack, onOpenEvent, onOpenResource }: { context: string; namespace: string; definition: ResourceDefinition; name: string; revision: number; reloadEpoch: number; writeEnabled: boolean; onBack: () => void; onOpenEvent: (event: EventSummary) => void; onOpenResource: (definition: ResourceDefinition, name: string, namespace?: string) => void }) {
  const [object, setObject] = useState<ResourceObject | null>(null)
  const [yamlText, setYamlText] = useState('')
  const [originalYaml, setOriginalYaml] = useState('')
  const [secretsRevealed, setSecretsRevealed] = useState(false)
  const [tab, setTab] = useState('overview')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [reviewReady, setReviewReady] = useState(false)
  const [reviewToken, setReviewToken] = useState<string | undefined>()
  const [conflict, setConflict] = useState<ResourceConflictResponse | null>(null)
  const [conflictReviewed, setConflictReviewed] = useState(false)
  const [draftKept, setDraftKept] = useState(false)
  const isSecret = definition.resource === 'secrets'
  const isNew = name === 'new'
  const schemaCandidate = !isSecret
  const [resourceSchema, setResourceSchema] = useState<ResourceSchemaResponse | null>(null)
  const [schemaLoading, setSchemaLoading] = useState(false)

  useEffect(() => {
    let active = true
    setConflict(null)
    setConflictReviewed(false)
    setDraftKept(false)
    setReviewReady(false)
    setReviewToken(undefined)
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
    setReviewReady(false)
    setReviewToken(undefined)
  }, [context, writeEnabled, reloadEpoch])

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
       if (conflict) {
         setError('Review the current cluster state and prepare the local draft before starting another review.')
         setTab('yaml')
         return
       }
       setReviewReady(false)
       setReviewToken(undefined)
       const response = await applyResource(context, definition, namespace, name, yamlText, true)
       if (!response.reviewToken) {
         setError('The server did not return a mutation review handle. Review the changes again.')
         return
       }
       setReviewReady(true)
       setReviewToken(response.reviewToken)
       setError('')
      } catch (reviewError) {
        setReviewReady(false)
        setReviewToken(undefined)
        if (!showResourceConflict(reviewError)) setError((reviewError as Error).message)
     } finally {
      setSaving(false)
    }
  }

  /** Applies a previously reviewed resource document to Kubernetes. */
  async function saveChanges() {
    if (!reviewToken) {
      setReviewReady(false)
      setError('Run the server-side dry-run before applying these changes.')
      return
    }
    setSaving(true)
    try {
       const response = await applyResource(context, definition, namespace, name, yamlText, false, reviewToken)
       const applied = response.value
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
       setReviewToken(undefined)
       setError(refreshError)
       setTab('overview')
      } catch (saveError) {
        setReviewReady(false)
        setReviewToken(undefined)
        if (!showResourceConflict(saveError)) setError((saveError as Error).message)
     } finally {
       setSaving(false)
     }
   }

   /** Moves an update conflict into the dedicated review state while retaining the local YAML draft. */
   function showResourceConflict(candidate: unknown): boolean {
     const nextConflict = resourceConflictFromError(candidate)
     if (!nextConflict) return false
     setConflict(nextConflict)
      setConflictReviewed(false)
      setDraftKept(false)
      setReviewReady(false)
      setReviewToken(undefined)
     setTab('yaml')
     setError('')
     return true
   }

   /** Keeps the conflict state visible while explicitly retaining the operator draft. */
   function keepConflictDraft() {
     setDraftKept(true)
     setError('Local draft retained. Review the current cluster snapshot before preparing another review.')
   }

   /** Replaces the editor with the safe current snapshot after explicit operator choice. */
   function reloadConflictObject() {
     if (!conflict?.currentObject) return
     const refreshed = sanitizeEditorObject(conflict.currentObject)
     const text = stringify(isSecret ? redactSecret(refreshed) : refreshed)
     setObject(conflict.currentObject)
     setYamlText(text)
      setOriginalYaml(text)
      setReviewReady(false)
      setReviewToken(undefined)
     setConflict(null)
     setConflictReviewed(false)
      setDraftKept(false)
      if (isSecret) setSecretsRevealed(false)
     setError('Current cluster YAML loaded. Review any new edits before applying.')
   }

   /** Copies the local draft to a bounded browser download without sending it to the backend. */
   function exportConflictDraft() {
     const blob = new Blob([yamlText], { type: 'application/yaml' })
     const url = URL.createObjectURL(blob)
     const anchor = document.createElement('a')
     anchor.href = url
     anchor.download = `${safeDownloadName(name)}-draft.yaml`
     anchor.click()
     window.setTimeout(() => URL.revokeObjectURL(url), 0)
   }

   /** Carries only the reviewed current resource version into the local draft before a new dry-run. */
   function prepareConflictRetry() {
     if (!conflict?.currentResourceVersion || !conflictReviewed) return
     const parsed = parseResourceObject(yamlText)
     if (!parsed) {
       setError('The local draft is not valid YAML. Fix it before preparing another review.')
       return
     }
     const nextObject = sanitizeEditorObject(parsed)
     const metadata = nextObject.metadata && typeof nextObject.metadata === 'object' && !Array.isArray(nextObject.metadata) ? nextObject.metadata as Record<string, unknown> : {}
     metadata.resourceVersion = conflict.currentResourceVersion
     nextObject.metadata = metadata
      setObject(nextObject)
      setYamlText(stringify(nextObject))
      setReviewReady(false)
      setReviewToken(undefined)
      setConflict(null)
     setConflictReviewed(false)
     setDraftKept(false)
     setError('Current resource version carried into the draft. Review changes again before applying.')
   }

   /** Refreshes the complete Pod after an operational subresource changes it. */
   async function refreshObject() {
     setReviewReady(false)
     setReviewToken(undefined)
     const refreshed = await fetchResource(context, definition, namespace, name)
    const editorObject = sanitizeEditorObject(refreshed)
    const text = stringify(isSecret ? redactSecret(editorObject) : editorObject)
    setObject(refreshed)
    setYamlText(text)
    setOriginalYaml(text)
    setReviewReady(false)
    setReviewToken(undefined)
    setError('')
  }

  /** Replaces the generic YAML editor document with one changed through the schema form. */
  function updateFormObject(nextObject: ResourceObject) {
    const safeObject = sanitizeEditorObject(nextObject)
    setObject(safeObject)
    setYamlText(stringify(safeObject))
    setReviewReady(false)
    setReviewToken(undefined)
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
  const conflictCurrentYaml = conflict?.currentObject ? boundedConflictText(stringify(isSecret ? redactSecret(sanitizeEditorObject(conflict.currentObject)) : sanitizeEditorObject(conflict.currentObject))) : ''
  const baseDetailTabs = definition.resource === 'events' ? ['overview', 'yaml', 'related'] : definition.resource === 'pods' && !isNew ? ['overview', 'debug', 'files', 'yaml', 'events', 'terminal', 'port-forward', 'logs', 'related'] : definition.resource === 'pods' ? ['overview', 'yaml', 'events', 'terminal', 'port-forward', 'logs', 'related'] : ['overview', 'yaml', 'events', 'related']
  const schemaTabs = schemaCandidate ? [baseDetailTabs[0], 'form', ...baseDetailTabs.slice(1)] : baseDetailTabs
  const detailTabs = !isNew ? [schemaTabs[0], ...(isOperationalReadPackResource(definition) ? ['read-pack'] : []), ...((targetedWorkloadResource(definition) || targetedNodeResource(definition) || targetedPVCResource(definition)) ? ['operations'] : []), ...schemaTabs.slice(1)] : schemaTabs
  const resourceNamespace = definition.namespaced ? metadataString(object, 'namespace') || (namespace === 'all' ? '' : namespace) : ''

  return <div className="detail-page">
    <div className="detail-breadcrumb"><button className="back-link" onClick={onBack}>← {titleCase(definition.resource)}</button><span>/</span><strong>{isNew ? 'new' : name}</strong></div>
     <section className="detail-heading"><div><div className="eyebrow"><span className="eyebrow-mark" /> {isNew ? 'Create resource' : 'Resource detail'}</div><h1>{isNew ? `New ${definition.kind}` : name}</h1><p>{definition.kind} / {namespace === 'all' || !definition.namespaced ? 'cluster-scoped' : namespace}</p></div><div className="detail-actions"><span className="status-pill status-good"><span /> {isNew ? 'draft' : 'loaded'}</span><button className="secondary-button" disabled={!writeEnabled || !dirty || saving || Boolean(conflict)} onClick={reviewChanges}><ShieldCheck size={15} /> Review changes</button></div></section>
     {definition.customResourceCapabilities && <CustomResourceCapabilityPanel capabilities={definition.customResourceCapabilities} />}
     {!isNew && <DeleteReviewPanel context={context} namespace={resourceNamespace || namespace} definition={definition} name={name} resourceVersion={metadataString(object, 'resourceVersion')} writeEnabled={writeEnabled} onDeleted={onBack} />}
     <div className="detail-tabs" role="tablist">{detailTabs.map((item) => <button key={item} className={tab === item ? 'detail-tab active' : 'detail-tab'} onClick={() => { if (item === 'form') syncObjectFromYaml(); setTab(item) }}>{item === 'yaml' ? 'YAML editor' : item === 'form' ? 'Schema form' : titleCase(item)}</button>)}</div>
     {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
        {conflict && <ConflictResolutionPanel conflict={conflict} currentYaml={conflictCurrentYaml} isSecret={isSecret} draftKept={draftKept} reviewed={conflictReviewed} onReviewed={setConflictReviewed} onKeepDraft={keepConflictDraft} onReload={reloadConflictObject} onExport={exportConflictDraft} onPrepareRetry={prepareConflictRetry} />}
         {tab === 'overview' && <div className="detail-grid"><div className="panel detail-panel"><PanelHeading eyebrow="Identity" title="Resource coordinates" action="Stable object" tone="good" /><div className="identity-list"><IdentityRow label="Kind" value={definition.kind} /><IdentityRow label="Namespace" value={namespace === 'all' || !definition.namespaced ? 'cluster-scoped' : namespace} /><IdentityRow label="UID" value={String(object.metadata && (object.metadata as Record<string, unknown>).uid || '—')} mono /><IdentityRow label="Resource version" value={String(object.metadata && (object.metadata as Record<string, unknown>).resourceVersion || '—')} mono /></div></div><div className="panel detail-panel"><PanelHeading eyebrow="Fields" title="Top-level shape" action={`${Object.keys(object).length} sections`} tone="neutral" /><div className="field-cloud">{Object.keys(object).map((key) => <span key={key}><span />{key}</span>)}</div></div></div>}
          {tab === 'read-pack' && isOperationalReadPackResource(definition) && <OperationalReadPackView context={context} namespace={resourceNamespace || namespace} definition={definition} name={name} revision={revision} reloadEpoch={reloadEpoch} />}
          {tab === 'operations' && targetedWorkloadResource(definition) && <WorkloadActionsPanel context={context} namespace={resourceNamespace || namespace} definition={definition} name={name} object={object} writeEnabled={writeEnabled} reloadEpoch={reloadEpoch} onRefresh={refreshObject} />}
          {tab === 'operations' && targetedNodeResource(definition) && <NodeOperationsPanel context={context} name={name} object={object} writeEnabled={writeEnabled} reloadEpoch={reloadEpoch} onRefresh={refreshObject} />}
          {tab === 'operations' && targetedPVCResource(definition) && <PVCOperationsPanel context={context} namespace={resourceNamespace || namespace} name={name} object={object} writeEnabled={writeEnabled} reloadEpoch={reloadEpoch} onRefresh={refreshObject} />}
     {tab === 'form' && schemaCandidate && <SchemaFormPanel response={resourceSchema} loading={schemaLoading} object={sanitizeEditorObject(object)} isNew={isNew} namespaced={definition.namespaced} namespace={namespace} writeEnabled={writeEnabled} reviewReady={reviewReady} onChange={updateFormObject} onOpenYaml={() => setTab('yaml')} />}
       {tab === 'yaml' && <div className="editor-panel"><div className="editor-toolbar"><div><span className="editor-dot" /><strong>{isSecret ? 'Secret YAML' : 'Resource YAML'}</strong><span>{dirty ? 'unsaved changes' : 'synced with cluster'}</span></div><div>{!writeEnabled && <span className="editor-lock"><LockKeyhole size={13} /> Enable write mode to edit</span>}{reviewReady && <span className="review-ready"><Check size={13} /> Dry-run passed</span>}</div></div>{isSecret && <div className="secret-notice"><LockKeyhole size={15} /><span>{secretsRevealed ? 'Secret values are visible in this editor.' : 'Secret values are masked by default.'}</span>{secretsRevealed ? <button className="reveal-button" onClick={() => { setSecretsRevealed(false); setReviewReady(false); setReviewToken(undefined); const safeText = stringify(redactSecret(object)); setYamlText(safeText); setOriginalYaml(safeText) }}>Mask values</button> : <button className="reveal-button" onClick={() => { setSecretsRevealed(true); setReviewReady(false); setReviewToken(undefined); const rawText = stringify(sanitizeEditorObject(object)); setYamlText(rawText); setOriginalYaml(rawText) }}>Reveal values</button>}</div>}<textarea className="yaml-editor" value={yamlText} onChange={(event) => { setYamlText(event.target.value); setReviewReady(false); setReviewToken(undefined); const parsed = parseResourceObject(event.target.value); if (parsed) setObject(sanitizeEditorObject(parsed)) }} readOnly={!writeEnabled || (isSecret && !secretsRevealed)} spellCheck={false} aria-label="Resource YAML" /><div className="editor-footer"><span><FileCode2 size={14} /> client-side apply / full object update</span>{reviewReady && writeEnabled && <button className="primary-button" onClick={saveChanges} disabled={saving}>{saving ? <LoaderCircle size={15} className="spin" /> : <Check size={15} />} Apply reviewed changes</button>}</div></div>}
      {tab === 'logs' && definition.resource === 'pods' && <PodLogsPanel context={context} namespace={namespace} pod={name} />}
      {tab === 'terminal' && definition.resource === 'pods' && <PodTerminalPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} />}
      {tab === 'port-forward' && definition.resource === 'pods' && <PodPortForwardPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} />}
      {tab === 'debug' && definition.resource === 'pods' && !isNew && <PodDebugPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} writeEnabled={writeEnabled} onRefresh={refreshObject} />}
      {tab === 'files' && definition.resource === 'pods' && !isNew && <PodFileCopyPanel context={context} namespace={resourceNamespace || namespace} pod={name} object={object} writeEnabled={writeEnabled} />}
      {tab === 'events' && <ResourceEventsPanel context={context} namespace={namespace} uid={metadataString(object, 'uid')} revision={revision} reloadEpoch={reloadEpoch} onOpenEvent={onOpenEvent} />}
      {tab === 'related' && <RelatedResourcesPanel context={context} definition={definition} namespace={resourceNamespace} name={name} isNew={isNew} onOpenResource={onOpenResource} />}
 </div>
}

/** Loads and presents one bounded built-in operational read pack without mutation controls. */
function OperationalReadPackView({ context, namespace, definition, name, revision, reloadEpoch }: { context: string; namespace: string; definition: ResourceDefinition; name: string; revision: number; reloadEpoch: number }) {
  const [response, setResponse] = useState<OperationalReadPackResponse | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [refreshNonce, setRefreshNonce] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError('')
    fetchOperationalReadPack(context, definition, namespace, name, controller.signal)
      .then((data) => {
        if (controller.signal.aborted) return
        setResponse(data)
      })
      .catch(() => {
        if (!controller.signal.aborted) setError('The structured read pack could not be loaded. Generic YAML, Events, and related resources remain available.')
      })
      .finally(() => !controller.signal.aborted && setLoading(false))
    return () => controller.abort()
  }, [context, definition.group, definition.version, definition.resource, definition.namespaced, namespace, name, revision, reloadEpoch, refreshNonce])

  if (loading && !response) return <div className="read-pack-panel"><div className="read-pack-loading"><LoaderCircle size={17} className="spin" /> Reading bounded operational fields</div></div>
  if (error && !response) return <div className="read-pack-panel"><div className="read-pack-header"><div className="logs-title"><Gauge size={16} /><strong>Operational read pack</strong></div><button type="button" className="secondary-button" onClick={() => setRefreshNonce((value) => value + 1)}>Retry</button></div><div className="read-pack-error"><AlertTriangle size={16} /><span>{error}</span></div></div>
  if (!response) return <div className="read-pack-panel"><QuietState icon={<Gauge size={22} />} title="Read pack is unavailable" detail="Generic YAML, Events, and related resources remain available for this resource." /></div>

  return <div className="read-pack-panel">
    <div className="read-pack-header"><div><div className="logs-title"><Gauge size={16} /><strong>Operational read pack</strong><span>read-only / bounded</span></div><p className="read-pack-intro">Native summaries are extracted from the exact discovered GVR. Missing APIs, RBAC limits, and provider-specific fields stay visible without blocking the generic detail tabs.</p></div><div className="read-pack-header-actions"><ReadPackStateChip state={response.state} /><button type="button" className="icon-button" onClick={() => setRefreshNonce((value) => value + 1)} aria-label="Refresh operational read pack" title="Refresh operational read pack"><RefreshCw size={15} /></button></div></div>
    <div className="read-pack-summary"><span>{response.message}</span><code>{response.diagnostics.queryCount}/{response.diagnostics.queryBudget} reads · cap {response.diagnostics.resultLimit}</code></div>
    {error && <div className="read-pack-error"><AlertTriangle size={16} /><span>{error}</span></div>}
    <div className="read-pack-cards">{response.panels.map((panel) => <OperationalReadPackCard key={panel.key} panel={panel} />)}</div>
    {loading && <div className="read-pack-refreshing"><LoaderCircle size={13} className="spin" /> Refreshing operational fields</div>}
  </div>
}

/** Renders one operational panel with an explicit state badge and structured safe fields. */
function OperationalReadPackCard({ panel }: { panel: OperationalReadPackPanelModel }) {
  const data = panel.data
  let content: ReactNode = <div className="read-pack-no-data">No structured fields are available for this panel.</div>
  if (panel.key === 'ingress' && data) content = <IngressReadPackView data={data as IngressReadPackData} />
  if (panel.key === 'endpoint-slice' && data) content = <EndpointSliceReadPackView data={data as EndpointSliceReadPackData} />
  if (panel.key === 'network-policy' && data) content = <NetworkPolicyReadPackView data={data as NetworkPolicyReadPackData} />
  if (panel.key === 'horizontal-pod-autoscaler' && data) content = <HorizontalPodAutoscalerReadPackView data={data as HorizontalPodAutoscalerReadPackData} />
  if (panel.key === 'pod-disruption-budget' && data) content = <PodDisruptionBudgetReadPackView data={data as PodDisruptionBudgetReadPackData} />
  if (panel.key === 'resource-quota' && data) content = <ResourceQuotaReadPackView data={data as ResourceQuotaReadPackData} />
  if (panel.key === 'limit-range' && data) content = <LimitRangeReadPackView data={data as LimitRangeReadPackData} />
  if (panel.key === 'persistent-volume' && data) content = <PersistentVolumeReadPackView data={data as PersistentVolumeReadPackData} />
  if (panel.key === 'storage-class' && data) content = <StorageClassReadPackView data={data as StorageClassReadPackData} />
  if (panel.key === 'service-account' && data) content = <ServiceAccountReadPackView data={data as ServiceAccountReadPackData} />
  if (panel.key === 'pod' && data) content = <PodReadPackView data={data as PodReadPackData} />
  return <section className={`read-pack-card read-pack-card-${panel.state}`}><div className="read-pack-card-heading"><div><span className="panel-eyebrow">Built-in view</span><h2>{panel.title}</h2></div><ReadPackStateChip state={panel.state} /></div><p className="read-pack-card-message">{panel.message}</p>{panel.truncated && <div className="read-pack-truncated"><AlertTriangle size={14} /><span>Results are capped. The generic related-resource view remains available for broader navigation.</span></div>}{content}</section>
}

/** Renders the compact state label used by every operational panel. */
function ReadPackStateChip({ state }: { state: string }) {
  return <span className={`read-pack-state-chip read-pack-state-${state}`}><span />{operationalReadPackStateLabel(state)}</span>
}

/** Renders a dense field grid shared by structured operational views. */
function ReadPackFieldGrid({ rows }: { rows: Array<{ label: string; value: ReactNode }> }) {
  return <div className="read-pack-field-grid">{rows.map((row) => <div className="read-pack-field" key={row.label}><span>{row.label}</span><strong>{row.value === undefined || row.value === null || row.value === '' ? '—' : row.value}</strong></div>)}</div>
}

/** Renders a titled subsection inside one operational card. */
function ReadPackSection({ title, children }: { title: string; children: ReactNode }) {
  return <div className="read-pack-section"><div className="read-pack-section-heading"><span>{title}</span></div>{children}</div>
}

/** Renders an explicit empty line for bounded structured collections. */
function ReadPackEmptyLine({ children }: { children: ReactNode }) {
  return <div className="read-pack-empty-line">{children}</div>
}

/** Renders a safe reference list without navigation or object bodies. */
function ReadPackReferenceList({ items, emptyLabel }: { items: OperationalReadPackReference[]; emptyLabel: string }) {
  if (!items.length) return <ReadPackEmptyLine>{emptyLabel}</ReadPackEmptyLine>
  return <div className="read-pack-reference-list">{items.map((item, index) => <div className="read-pack-reference" key={`${item.kind || 'resource'}-${item.namespace || ''}-${item.name}-${index}`}><div><strong>{item.kind || 'Resource'}/{item.name || 'unnamed'}</strong><span>{item.namespace ? `ns ${item.namespace}` : 'cluster scope'}</span></div><span className={`read-pack-reference-state read-pack-state-${item.state}`}>{operationalReadPackStateLabel(item.state)}</span></div>)}</div>
}

/** Renders bounded condition summaries without backend error messages. */
function ReadPackConditions({ conditions }: { conditions: OperationalCondition[] }) {
  if (!conditions.length) return <ReadPackEmptyLine>No condition rows were returned.</ReadPackEmptyLine>
  return <div className="read-pack-condition-list">{conditions.map((condition, index) => <div className="read-pack-condition" key={`${condition.type}-${index}`}><span className={`read-pack-condition-status read-pack-condition-${condition.status.toLowerCase()}`} /> <strong>{condition.type || 'Condition'}</strong><code>{condition.status || 'unknown'}{condition.reason ? ` / ${condition.reason}` : ''}</code></div>)}</div>
}

/** Renders a map of bounded quantity values. */
function ReadPackMap({ values }: { values: Record<string, string> }) {
  const entries = Object.entries(values)
  if (!entries.length) return <span className="read-pack-muted">none</span>
  return <span className="read-pack-map">{entries.slice(0, 8).map(([key, value]) => <code key={key}>{key}={value}</code>)}</span>
}

/** Renders Ingress service backend, rule, and EndpointSlice visibility. */
function IngressReadPackView({ data }: { data: IngressReadPackData }) {
  return <div className="read-pack-body"><ReadPackFieldGrid rows={[{ label: 'Ingress class', value: data.ingressClass }, { label: 'Rules', value: data.rules.length }, { label: 'Backends', value: data.backends.length }, { label: 'EndpointSlices', value: data.endpointSlices.length }]} /><ReadPackSection title="Backends"><div className="read-pack-table">{data.backends.length ? data.backends.map((backend, index) => <div className="read-pack-table-row" key={`${backend.name}-${backend.port}-${index}`}><strong>{backend.kind || 'Service'}/{backend.name}</strong><code>{backend.port || 'any port'}</code><span>{backend.state}</span></div>) : <ReadPackEmptyLine>No service backends were declared.</ReadPackEmptyLine>}</div></ReadPackSection><ReadPackSection title="Rules and TLS"><div className="read-pack-rule-list">{data.rules.map((rule, index) => <div className="read-pack-rule" key={`${rule.host || 'default'}-${index}`}><strong>{rule.host || '* default host'}</strong><span>{rule.paths.map((path) => `${path.path || '/'} → ${path.serviceName || path.resource || 'unresolved'}${path.servicePort ? `:${path.servicePort}` : ''}`).join(' · ') || 'no HTTP paths'}</span></div>)}{data.tlsHosts.length > 0 && <div className="read-pack-rule"><strong>TLS hosts</strong><span>{data.tlsHosts.join(' · ')}</span></div>}{!data.rules.length && !data.tlsHosts.length && <ReadPackEmptyLine>No rules or TLS hosts were returned.</ReadPackEmptyLine>}</div></ReadPackSection><ReadPackSection title="EndpointSlice visibility"><div className="read-pack-table">{data.endpointSlices.length ? data.endpointSlices.map((slice) => <div className="read-pack-table-row" key={slice.name}><strong>{slice.name}</strong><code>{slice.serviceName || 'unlabeled'}</code><span>{slice.endpoints.length} endpoint{slice.endpoints.length === 1 ? '' : 's'}</span></div>) : <ReadPackEmptyLine>No matching EndpointSlices were returned.</ReadPackEmptyLine>}</div></ReadPackSection></div>
}

/** Renders one EndpointSlice's addresses, ports, readiness, and topology. */
function EndpointSliceReadPackView({ data }: { data: EndpointSliceReadPackData }) {
  const slice = data.slice
  return <div className="read-pack-body"><ReadPackFieldGrid rows={[{ label: 'Service', value: slice.serviceName }, { label: 'Address type', value: slice.addressType }, { label: 'Ports', value: slice.ports.join(', ') }, { label: 'Endpoints', value: slice.endpoints.length }]} /><ReadPackSection title="Endpoint readiness"><div className="read-pack-table">{slice.endpoints.length ? slice.endpoints.map((endpoint, index) => <div className="read-pack-table-row" key={`${endpoint.addresses.join('-')}-${index}`}><strong>{endpoint.addresses.join(', ') || 'no address'}</strong><code>{endpoint.nodeName || endpoint.zone || 'no topology'}</code><span>{endpoint.ready === undefined ? 'unknown' : endpoint.ready ? 'ready' : 'not ready'}</span></div>) : <ReadPackEmptyLine>No addresses were returned.</ReadPackEmptyLine>}</div></ReadPackSection></div>
}

/** Renders NetworkPolicy selectors, rule shape, and affected Pod references. */
function NetworkPolicyReadPackView({ data }: { data: NetworkPolicyReadPackData }) {
  return <div className="read-pack-body"><ReadPackFieldGrid rows={[{ label: 'Pod selector', value: data.podSelector.text || 'all Pods in namespace' }, { label: 'Policy types', value: data.policyTypes.join(', ') }, { label: 'Ingress rules', value: data.ingressRules.length }, { label: 'Egress rules', value: data.egressRules.length }, { label: 'Affected Pods', value: data.affectedPods.length }]} /><ReadPackSection title="Rule shape"><div className="read-pack-table">{[...data.ingressRules.map((rule, index) => ({ ...rule, label: `Ingress ${index + 1}` })), ...data.egressRules.map((rule, index) => ({ ...rule, label: `Egress ${index + 1}` }))].map((rule) => <div className="read-pack-table-row" key={rule.label}><strong>{rule.label}</strong><code>{rule.peerCount} peers</code><span>{rule.portCount} ports</span></div>)}{!data.ingressRules.length && !data.egressRules.length && <ReadPackEmptyLine>No ingress or egress rules were returned.</ReadPackEmptyLine>}</div></ReadPackSection><ReadPackSection title="Affected Pods"><ReadPackReferenceList items={data.affectedPods} emptyLabel="No matching Pods were returned by the bounded selector query." /></ReadPackSection></div>
}

/** Renders HPA target, replica status, metrics, and conditions. */
function HorizontalPodAutoscalerReadPackView({ data }: { data: HorizontalPodAutoscalerReadPackData }) {
  return <div className="read-pack-body"><ReadPackFieldGrid rows={[{ label: 'Target', value: `${data.target.kind || 'Resource'}/${data.target.name || 'unresolved'}` }, { label: 'Min replicas', value: data.minReplicas }, { label: 'Max replicas', value: data.maxReplicas }, { label: 'Current', value: data.currentReplicas }, { label: 'Desired', value: data.desiredReplicas }]} /><ReadPackSection title="Metrics"><div className="read-pack-table">{data.metrics.length ? data.metrics.map((metric, index) => <div className="read-pack-table-row" key={`${metric.type}-${metric.resource || metric.name || index}`}><strong>{metric.type || 'metric'}</strong><code>{metric.resource || metric.name || 'custom'}</code><span>{metric.current || metric.target || metric.targetAverage || (metric.targetUtilization === undefined ? 'no reading' : `${metric.targetUtilization}% target`)}</span></div>) : <ReadPackEmptyLine>No HPA metric rows were returned.</ReadPackEmptyLine>}</div></ReadPackSection><ReadPackSection title="Conditions"><ReadPackConditions conditions={data.conditions} /></ReadPackSection></div>
}

/** Renders PDB availability counters and disruption allowance. */
function PodDisruptionBudgetReadPackView({ data }: { data: PodDisruptionBudgetReadPackData }) {
  return <div className="read-pack-body"><ReadPackFieldGrid rows={[{ label: 'Min available', value: data.minAvailable }, { label: 'Max unavailable', value: data.maxUnavailable }, { label: 'Expected Pods', value: data.expectedPods }, { label: 'Current healthy', value: data.currentHealthy }, { label: 'Desired healthy', value: data.desiredHealthy }, { label: 'Disruptions allowed', value: data.disruptionsAllowed }]} /><ReadPackSection title="Status conditions"><ReadPackConditions conditions={data.conditions} /></ReadPackSection></div>
}

/** Renders ResourceQuota used and hard quantity summaries. */
function ResourceQuotaReadPackView({ data }: { data: ResourceQuotaReadPackData }) {
  return <div className="read-pack-body"><ReadPackFieldGrid rows={[{ label: 'Scopes', value: data.scopes.join(', ') || 'all scopes' }, { label: 'Resources', value: data.usage.length }]} /><ReadPackSection title="Usage"><div className="read-pack-table">{data.usage.length ? data.usage.map((item) => <div className="read-pack-table-row" key={item.resource}><strong>{item.resource}</strong><code>{item.used || '—'} used / {item.hard || '—'} hard</code><span>{item.percent === undefined ? '—' : `${item.percent}%`}</span></div>) : <ReadPackEmptyLine>No used or hard quantities were returned.</ReadPackEmptyLine>}</div></ReadPackSection></div>
}

/** Renders LimitRange default and bound quantities. */
function LimitRangeReadPackView({ data }: { data: LimitRangeReadPackData }) {
  return <div className="read-pack-body"><ReadPackSection title="Limit items"><div className="read-pack-table">{data.items.length ? data.items.map((item, index) => <div className="read-pack-table-row read-pack-table-row-stack" key={`${item.type}-${index}`}><strong>{item.type || 'limit'}</strong><ReadPackMap values={{ ...item.default, ...item.defaultRequest }} /><ReadPackMap values={{ ...item.min, ...item.max }} /></div>) : <ReadPackEmptyLine>No LimitRange items were returned.</ReadPackEmptyLine>}</div></ReadPackSection></div>
}

/** Renders PersistentVolume lifecycle fields and relationship references. */
function PersistentVolumeReadPackView({ data }: { data: PersistentVolumeReadPackData }) {
  return <div className="read-pack-body"><ReadPackFieldGrid rows={[{ label: 'Phase', value: data.phase }, { label: 'Reason', value: data.reason }, { label: 'StorageClass', value: data.storageClass }, { label: 'Reclaim policy', value: data.reclaimPolicy }, { label: 'Volume mode', value: data.volumeMode }, { label: 'Source', value: data.source }, { label: 'Capacity', value: <ReadPackMap values={data.capacity} /> }, { label: 'Provider message', value: data.providerMessage ? 'present but withheld' : 'not reported' }]} /><ReadPackSection title="Relationships"><ReadPackReferenceList items={data.relationships} emptyLabel="No claim or StorageClass relationship was declared." /></ReadPackSection></div>
}

/** Renders StorageClass lifecycle settings and bounded PersistentVolume relationships. */
function StorageClassReadPackView({ data }: { data: StorageClassReadPackData }) {
  return <div className="read-pack-body"><ReadPackFieldGrid rows={[{ label: 'Provisioner', value: data.provisioner }, { label: 'Reclaim policy', value: data.reclaimPolicy }, { label: 'Binding mode', value: data.volumeBindingMode }, { label: 'Expansion', value: data.allowVolumeExpansion === undefined ? 'not reported' : data.allowVolumeExpansion ? 'allowed' : 'disabled' }, { label: 'Mount options', value: data.mountOptions.join(', ') || 'none' }, { label: 'Allowed topologies', value: data.allowedTopologies }]} /><ReadPackSection title="PersistentVolumes"><ReadPackReferenceList items={data.persistentVolumes} emptyLabel="No PersistentVolumes use this StorageClass in the bounded result." /></ReadPackSection></div>
}

/** Renders ServiceAccount references and read-only RBAC bindings. */
function ServiceAccountReadPackView({ data }: { data: ServiceAccountReadPackData }) {
  const bindings = [...data.roleBindings.map((binding) => ({ ...binding, kind: 'RoleBinding' })), ...data.clusterRoleBindings.map((binding) => ({ ...binding, kind: 'ClusterRoleBinding' }))]
  return <div className="read-pack-body"><ReadPackFieldGrid rows={[{ label: 'Automount token', value: data.automountServiceAccountToken === undefined ? 'not reported' : data.automountServiceAccountToken ? 'enabled' : 'disabled' }, { label: 'Secret references', value: data.secrets.join(', ') || 'none' }, { label: 'Image pull refs', value: data.imagePullSecrets.join(', ') || 'none' }, { label: 'RBAC bindings', value: bindings.length }]} /><ReadPackSection title="RBAC bindings"><div className="read-pack-table">{bindings.length ? bindings.map((binding, index) => <div className="read-pack-table-row read-pack-table-row-stack" key={`${binding.kind}-${binding.name}-${index}`}><strong>{binding.kind}/{binding.name}</strong><code>{binding.roleKind || 'Role'}/{binding.roleName || 'unresolved'}</code><span>{binding.namespace || 'cluster scope'}</span></div>) : <ReadPackEmptyLine>No read-only RoleBinding or ClusterRoleBinding references this account.</ReadPackEmptyLine>}</div></ReadPackSection></div>
}

/** Renders Pod scheduling, readiness, containers, volumes, and security posture. */
function PodReadPackView({ data }: { data: PodReadPackData }) {
  const containers = [...data.containers, ...data.initContainers.map((container) => ({ ...container, name: `init/${container.name}` })), ...data.ephemeralContainers.map((container) => ({ ...container, name: `debug/${container.name}` }))]
  return <div className="read-pack-body"><ReadPackFieldGrid rows={[{ label: 'Phase', value: data.phase }, { label: 'Reason', value: data.reason }, { label: 'Node', value: data.nodeName }, { label: 'Scheduler', value: data.schedulerName }, { label: 'Pod IP', value: data.podIP }, { label: 'Readiness gates', value: data.readinessGates.join(', ') || 'none' }]} /><ReadPackSection title="Conditions"><ReadPackConditions conditions={data.conditions} /></ReadPackSection><ReadPackSection title="Containers"><div className="read-pack-table">{containers.length ? containers.map((container) => <PodContainerReadPackRow container={container} key={container.name} />) : <ReadPackEmptyLine>No container summaries were returned.</ReadPackEmptyLine>}</div></ReadPackSection><ReadPackSection title="Volumes"><div className="read-pack-table">{data.volumes.length ? data.volumes.map((volume) => <div className="read-pack-table-row" key={volume.name}><strong>{volume.name}</strong><code>{volume.type}</code><span>{volume.reference || 'configured'}{volume.readOnly ? ' / read-only' : ''}</span></div>) : <ReadPackEmptyLine>No volume summaries were returned.</ReadPackEmptyLine>}</div></ReadPackSection><ReadPackSection title="Security context"><ReadPackFieldGrid rows={[{ label: 'Run as user', value: data.securityContext.runAsUser }, { label: 'Run as non-root', value: data.securityContext.runAsNonRoot === undefined ? 'not reported' : data.securityContext.runAsNonRoot ? 'yes' : 'no' }, { label: 'Seccomp', value: data.securityContext.seccompProfileType }, { label: 'Host network', value: data.securityContext.hostNetwork === undefined ? 'not reported' : data.securityContext.hostNetwork ? 'yes' : 'no' }]} /></ReadPackSection></div>
}

/** Renders one Pod container's readiness, probes, restarts, and termination reason. */
function PodContainerReadPackRow({ container }: { container: PodContainerSummary }) {
  const probes = container.probes.map((probe) => `${probe.name}${probe.handler ? ` / ${probe.handler}` : ''}`).join(', ')
  const termination = container.lastTerminationReason || container.stateReason || container.state || 'no termination reason'
  return <div className="read-pack-table-row read-pack-table-row-stack"><strong>{container.name}</strong><code>{container.image || 'image unavailable'}</code><span>{container.ready === undefined ? 'ready unknown' : container.ready ? 'ready' : 'not ready'} · {container.restartCount ?? 0} restarts · {termination}</span><small>{probes || 'no probes configured'}</small></div>
}

/** Renders bounded CRD identity, version, and subresource capability metadata on detail pages. */
function CustomResourceCapabilityPanel({ capabilities }: { capabilities: CustomResourceCapabilities }) {
  const scalePaths = [capabilities.scaleSpecReplicasPath, capabilities.scaleStatusReplicasPath, capabilities.scaleLabelSelectorPath].filter(Boolean).join(' / ')
  return <section className="crd-capability-panel"><div className="crd-capability-heading"><div><span className="panel-eyebrow">Custom resource definition</span><strong>{capabilities.group} / {capabilities.kind}</strong></div><code>{capabilities.resource} / {capabilities.scope}</code></div><div className="crd-capability-grid"><div><span>Version identity</span><strong>{capabilities.version}</strong><small>{customResourceCapabilityLabel(capabilities)}</small></div><div><span>Subresources</span><strong>{capabilities.statusSubresource || capabilities.scaleSubresource ? [capabilities.statusSubresource ? 'status' : '', capabilities.scaleSubresource ? 'scale' : ''].filter(Boolean).join(' / ') : 'none declared'}</strong><small>{scalePaths ? `scale paths ${scalePaths}` : 'No custom-resource actions are added here.'}</small></div><div><span>Printer columns</span><strong>{capabilities.printerColumns?.length ?? 0} safe columns</strong><small>{capabilities.printerColumnWarnings?.length ? 'Unsupported columns use generic table fields.' : 'Values are bounded and evaluated by the backend.'}</small></div></div></section>
}

/** Renders the explicit current-state review and local-draft choices for a resource-version conflict. */
function ConflictResolutionPanel({ conflict, currentYaml, isSecret, draftKept, reviewed, onReviewed, onKeepDraft, onReload, onExport, onPrepareRetry }: { conflict: ResourceConflictResponse; currentYaml: string; isSecret: boolean; draftKept: boolean; reviewed: boolean; onReviewed: (reviewed: boolean) => void; onKeepDraft: () => void; onReload: () => void; onExport: () => void; onPrepareRetry: () => void }) {
  const currentReady = conflict.currentAvailable && Boolean(conflict.currentResourceVersion) && Boolean(currentYaml)
  const changedSections = conflict.summary.changedSections || []
  const localVersion = conflict.summary.localResourceVersion || 'not supplied'
  const currentVersion = conflict.currentResourceVersion || 'unavailable'
  return <section className="conflict-panel" role="alert">
    <div className="conflict-heading"><div><span className="eyebrow"><span className="eyebrow-mark" /> Concurrency stop</span><h2>Cluster changed before this update</h2></div><span className="status-pill status-warning"><span /> No overwrite</span></div>
    <p className="conflict-intro">Kubernetes rejected the stale resource version. Your local draft remains in the editor below. Review the safe current snapshot, then choose whether to reload it or carry the reviewed current version into this draft.</p>
    <div className="conflict-version-grid"><div><span>Local draft version</span><code>{localVersion === 'not supplied' ? localVersion : `rv/${localVersion}`}</code></div><div><span>Current cluster version</span><code>{currentVersion === 'unavailable' ? currentVersion : `rv/${currentVersion}`}</code></div></div>
    <div className="conflict-summary"><strong>Safe difference summary</strong><span>{changedSections.length ? changedSections.join(', ') : 'No safe top-level field differences were identified.'}{conflict.summary.truncated ? ' and more' : ''}</span></div>
    {currentReady ? <div className="conflict-current"><div className="conflict-current-heading"><strong>Current cluster YAML</strong><span>read-only snapshot / bounded view</span></div><pre>{currentYaml}</pre></div> : <div className="conflict-current conflict-current-unavailable"><AlertTriangle size={16} /><div><strong>Current cluster snapshot unavailable</strong><span>Refresh the resource before retrying. The local draft is still preserved and can be exported.</span></div></div>}
    {isSecret && <div className="conflict-secret-note"><LockKeyhole size={14} /><span>Secret data and stringData values stay redacted in this conflict workflow. Reloading the current Secret YAML is disabled so masked placeholders cannot be submitted as values.</span></div>}
    <label className="conflict-review-check"><input type="checkbox" checked={reviewed} onChange={(event) => onReviewed(event.target.checked)} disabled={!currentReady} /><span>I reviewed the current cluster snapshot and understand the local draft may contain changes made after it.</span></label>
    <div className="conflict-actions"><button type="button" className="secondary-button" onClick={onKeepDraft}><FileCode2 size={14} /> {draftKept ? 'Draft retained' : 'Keep local draft'}</button><button type="button" className="secondary-button" onClick={onExport}><Download size={14} /> Export local draft</button><button type="button" className="secondary-button" onClick={onReload} disabled={!currentReady || isSecret}><RefreshCw size={14} /> Reload current YAML</button><button type="button" className="primary-button" onClick={onPrepareRetry} disabled={!currentReady || !reviewed}><ShieldCheck size={14} /> Prepare draft for re-review</button></div>
  </section>
}

/** Renders a normalized Kubernetes resource form while keeping YAML as the complete editing fallback. */
function SchemaFormPanel({ response, loading, object, isNew, namespaced, namespace, writeEnabled, reviewReady, onChange, onOpenYaml }: { response: ResourceSchemaResponse | null; loading: boolean; object: ResourceObject; isNew: boolean; namespaced: boolean; namespace: string; writeEnabled: boolean; reviewReady: boolean; onChange: (object: ResourceObject) => void; onOpenYaml: () => void }) {
  if (loading && !response) {
    return <div className="schema-form-panel"><div className="schema-form-loading"><LoaderCircle size={18} className="spin" /> Reading the version-specific OpenAPI schema</div></div>
  }
  if (!response || response.state !== 'available' || !response.schema) {
    return <div className="schema-form-panel"><div className="schema-form-header"><div className="logs-title"><FileCode2 size={16} /><strong>Schema form unavailable</strong><span>YAML remains complete</span></div><span className="schema-state-chip schema-state-warning">{schemaStateLabel(response?.state)}</span></div><div className="schema-form-fallback"><AlertTriangle size={18} /><div><strong>{schemaStateTitle(response?.state)}</strong><span>{response?.message || 'The Kubernetes API server did not provide a usable schema for this resource.'}</span></div></div><div className="schema-form-footer"><span>Generic CRUD and deep links remain available.</span><button type="button" className="secondary-button" onClick={onOpenYaml}><FileCode2 size={14} /> Open YAML editor</button></div></div>
  }

  const validationErrors = validateSchemaDocument(response.schema, object)
  const metadata = object.metadata && typeof object.metadata === 'object' && !Array.isArray(object.metadata) ? object.metadata as Record<string, unknown> : {}
  const metadataName = typeof metadata.name === 'string' ? metadata.name : ''
  const metadataNamespace = typeof metadata.namespace === 'string' ? metadata.namespace : namespace === 'all' ? '' : namespace
  const fallbackText = response.fallbacks?.length ? schemaFallbackSummary(response.fallbacks, response.fallbacksTruncated) : ''
  const statusNotice = schemaStatusNotice(response)
  const formSchema = projectSchemaIdentityFields(response.schema)
  const updatePath = (path: string[], value: unknown) => onChange(updateSchemaObject(object, path, value))
  return <div className="schema-form-panel">
     <div className="schema-form-header"><div className="logs-title"><FileCode2 size={16} /><strong>{isNew ? 'Create from schema' : 'Edit from schema'}</strong><span>{schemaSourceLabel(response.source)}</span></div><div className="schema-form-header-meta">{reviewReady && <span className="review-ready"><Check size={13} /> Dry-run passed</span>}<span className="schema-state-chip schema-state-good">Form ready</span></div></div>
     <div className="schema-form-intro"><strong>Fields are generated from the Kubernetes API schema.</strong><span>Nested objects, arrays, typed maps, required fields, defaults, enums, descriptions, and common validation constraints are shown here. YAML remains the complete fallback for fields the form does not model.</span><span>Identity fields are controlled above while metadata details are generated from the API schema; labels, annotations, generateName, and other metadata fields appear here when exposed.</span>{response.message && <span>{response.message}</span>}</div>
    {statusNotice && <div className="schema-readonly-notice"><LockKeyhole size={15} /><span>{statusNotice}</span></div>}
    {fallbackText && <div className="schema-partial-fallback"><AlertTriangle size={15} /><div><strong>Some fields use the YAML fallback</strong><span>{fallbackText}</span></div><button type="button" className="text-button" onClick={onOpenYaml}>Open YAML</button></div>}
    {!writeEnabled && <div className="operation-lock"><LockKeyhole size={14} /> Enable write mode to edit this form. Review still performs the existing Kubernetes server-side dry-run.</div>}
    <form className="schema-form" onSubmit={(event) => event.preventDefault()}>
      <fieldset className="schema-object schema-metadata" disabled={!writeEnabled}>
        <legend>Metadata</legend>
        <div className="schema-field-grid">
          <label className="schema-field"><span>Name <em>{isNew ? 'required for form create' : 'identity'}</em></span><input value={metadataName} onChange={(event) => updatePath(['metadata', 'name'], event.target.value || undefined)} readOnly={!isNew} required={isNew} /></label>
          {namespaced && <label className="schema-field"><span>Namespace {isNew && namespace === 'all' && <b>required</b>}</span><input value={metadataNamespace} onChange={(event) => updatePath(['metadata', 'namespace'], event.target.value || undefined)} readOnly={!isNew} required={isNew && namespace === 'all'} /></label>}
        </div>
      </fieldset>
      <SchemaFieldEditor field={formSchema} value={object} path={[]} writeEnabled={writeEnabled} onChange={updatePath} />
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
  const scalarUnion = Boolean(field.scalarTypes?.length)
  const valueKey = displayValue === undefined ? '' : schemaValueKey(displayValue)
  const stringValue = typeof displayValue === 'string' ? displayValue : displayValue === undefined || displayValue === null ? '' : String(displayValue)
  return <label className={`schema-field schema-field-${field.type}`}>
    <span>{label} {field.required && <b>required</b>}{field.readOnly && <em>read-only</em>}{present && !field.required && <button type="button" className="schema-clear" onClick={(event) => { event.preventDefault(); clear() }} disabled={!writeEnabled || field.readOnly}>Clear</button>}</span>
    {field.description && <small>{field.description}</small>}
     {enumValues.length > 0 && !scalarUnion ? <select value={valueKey} onChange={(event) => onChange(path, event.target.value ? parseSchemaValue(event.target.value) : undefined)} disabled={!writeEnabled || field.readOnly} required={field.required}><option value="">{field.required ? 'Choose a value' : 'Not set'}</option>{enumValues.map((item) => <option key={schemaValueKey(item)} value={schemaValueKey(item)}>{String(item)}</option>)}</select> : field.type === 'boolean' ? <input type="checkbox" checked={displayValue === true} onChange={(event) => onChange(path, event.target.checked)} disabled={!writeEnabled || field.readOnly} /> : <input type={field.type === 'number' || field.type === 'integer' ? 'number' : 'text'} value={stringValue} onChange={(event) => onChange(path, parseSchemaInput(field, event.target.value))} disabled={!writeEnabled || field.readOnly} required={field.required} min={field.minimum} max={field.maximum} minLength={field.minLength} maxLength={field.maxLength} pattern={field.pattern} step={field.type === 'integer' ? 1 : field.multipleOf || 'any'} inputMode={field.type === 'number' || field.type === 'integer' ? 'decimal' : undefined} />}
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
  if (state === 'unavailable') return 'API unavailable'
  if (state === 'malformed') return 'Malformed schema'
  if (state === 'error') return 'Read failed'
  return state === 'available' ? 'Available' : 'Reading'
}

/** Returns a direct explanation for one schema fallback state. */
function schemaStateTitle(state?: ResourceSchemaResponse['state']): string {
  if (state === 'permission-denied') return 'Schema access is denied by Kubernetes RBAC'
  if (state === 'non-structural') return 'This resource has no structural schema'
  if (state === 'unsupported') return 'This schema needs YAML'
  if (state === 'missing') return 'No matching OpenAPI schema'
  if (state === 'unavailable') return 'The OpenAPI API is unavailable'
  if (state === 'malformed') return 'The API returned a malformed schema'
  if (state === 'error') return 'The resource schema could not be read'
  return 'Schema form is loading'
}

/** Returns the authenticated schema source shown beside a ready form. */
function schemaSourceLabel(source?: ResourceSchemaResponse['source']): string {
  if (source === 'openapi-v2') return 'OpenAPI v2 compatibility'
  if (source === 'crd') return 'CRD schema fallback'
  return 'OpenAPI v3 / version-specific'
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
    case 'integer':
    case 'number':
      errors.push(...validateSchemaScalarValue(field, value, path))
      break
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
  const [reviewToken, setReviewToken] = useState<string | undefined>()
  const [confirmed, setConfirmed] = useState(false)
  const [confirmationText, setConfirmationText] = useState('')
  const [busy, setBusy] = useState<'review' | 'apply' | ''>('')
  const [error, setError] = useState('')

  useEffect(() => {
    setReview(null)
    setReviewToken(undefined)
    setConfirmed(false)
    setConfirmationText('')
    if (!writeEnabled) setError('')
  }, [context, namespace, definition.group, definition.version, definition.resource, name, resourceVersion, writeEnabled])

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
      const response = await deleteResource(context, definition, namespace, name, resourceVersion, true, false, '')
      if (!response.reviewToken) {
        setReview(null)
        setReviewToken(undefined)
        setError('The server did not return a mutation review handle. Review the delete again.')
        return
      }
      setReview(response.value)
      setReviewToken(response.reviewToken)
      setConfirmed(false)
      setConfirmationText('')
    } catch (reviewError) {
      setReview(null)
      setReviewToken(undefined)
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
    if (!review || review.status !== 'dry-run passed' || !reviewToken) {
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
      await deleteResource(context, definition, namespace, name, resourceVersion, false, true, confirmationText.trim(), reviewToken)
      setReview(null)
      setReviewToken(undefined)
      onDeleted()
    } catch (applyError) {
      setReview(null)
      setReviewToken(undefined)
      setConfirmed(false)
      setConfirmationText('')
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
       <div className="delete-review-actions"><button type="button" className="secondary-button" onClick={() => { setReview(null); setReviewToken(undefined); setConfirmed(false); setConfirmationText(''); setError('') }} disabled={Boolean(busy)}>Change review</button><button type="button" className="danger-button primary-button" onClick={() => void applyReviewedDelete()} disabled={!canApply}>{busy === 'apply' ? <LoaderCircle size={15} className="spin" /> : <Trash2 size={15} />} {busy === 'apply' ? 'Deleting' : 'Confirm and delete'}</button></div></div>}
    {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
  </div>
}

/** Renders discovery-backed relationships and explicit read limitations for a resource. */
function RelatedResourcesPanel({ context, definition, namespace, name, isNew, onOpenResource }: { context: string; definition: ResourceDefinition; namespace: string; name: string; isNew: boolean; onOpenResource: (definition: ResourceDefinition, name: string, namespace?: string) => void }) {
  const [response, setResponse] = useState<RelatedResourcesResponse | null>(null)
  const [reverseResponse, setReverseResponse] = useState<RelatedResourcesResponse | null>(null)
  const [loading, setLoading] = useState(!isNew)
  const [reverseLoading, setReverseLoading] = useState(false)
  const [error, setError] = useState('')
  const [reverseError, setReverseError] = useState('')
  const reverseRequestID = useRef(0)
  const reverseSupported = !(definition.group === '' && definition.version === 'v1' && definition.resource === 'events')

  useEffect(() => {
    setResponse(null)
    setReverseResponse(null)
    setReverseLoading(false)
    setError('')
    setReverseError('')
    reverseRequestID.current += 1
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
      reverseRequestID.current += 1
    }
  }, [context, definition, namespace, name, isNew])

  /** Loads supported reverse-owner relationships without delaying the direct relationship response. */
  async function loadReverseRelationships() {
    if (!reverseSupported) return
    const requestID = reverseRequestID.current + 1
    reverseRequestID.current = requestID
    setReverseLoading(true)
    setReverseError('')
    try {
      const data = await fetchRelatedReverse(context, definition, namespace, name)
      if (requestID !== reverseRequestID.current) return
      setReverseResponse(data)
    } catch (loadError) {
      if (requestID === reverseRequestID.current) setReverseError((loadError as Error).message)
    } finally {
      if (requestID === reverseRequestID.current) setReverseLoading(false)
    }
  }

  if (isNew) return <QuietState icon={<Network size={22} />} title="Save before reading relationships" detail="A new resource has no Kubernetes identity or native relationships until it is created." />
  if (loading && !response) return <div className="relationship-panel"><div className="panel relationship-summary-panel"><div className="relationship-loading"><LoaderCircle size={17} className="spin" /> Reading native relationships</div></div></div>
  if (error && !response) return <div className="relationship-panel"><div className="panel relationship-summary-panel"><PanelHeading eyebrow="Relationships" title="Related resources" action="Read failed" tone="warning" /><div className="inline-error"><AlertTriangle size={15} />{error}</div></div></div>

  const displayedResponse = response && reverseResponse ? mergeRelatedResponses(response, reverseResponse) : response
  const items = displayedResponse?.items ?? []
  const notices = displayedResponse?.notices ?? []
  const reverseDeferred = reverseSupported && Boolean(response?.notices?.some((notice) => notice.deferred)) && !reverseResponse
  return <div className="relationship-panel">
    <div className="panel relationship-summary-panel">
      <PanelHeading eyebrow="Relationships" title="Related resources" action={relatedAggregateLabel(displayedResponse?.state || 'empty', items.length)} tone={relatedAggregateTone(displayedResponse?.state || 'empty')} />
      <p className="relationship-copy">Links are derived from Kubernetes owner metadata, native selectors, binding fields, and explicit PodSpec references. Only sanitized resource coordinates are returned; Secret values are never read into this view.</p>
      {error && <div className="inline-error"><AlertTriangle size={15} />{error}</div>}
      {reverseDeferred && <button type="button" className="secondary-button" onClick={() => void loadReverseRelationships()} disabled={reverseLoading}>{reverseLoading ? <><LoaderCircle size={14} className="spin" /> Reading supported descendants</> : <><Network size={14} /> Load supported descendants</>}</button>}
      {reverseError && <div className="inline-error"><AlertTriangle size={15} />{reverseError}</div>}
      {displayedResponse?.diagnostics && <div className="panel-note"><Activity size={13} /> {displayedResponse.diagnostics.queryCount}/{displayedResponse.diagnostics.queryBudget} relationship reads · {displayedResponse.diagnostics.durationMilliseconds} ms · {displayedResponse.diagnostics.concurrentLimit} concurrent max</div>}
    </div>
    {notices.length ? <div className="relationship-notices">{notices.map((notice, index) => <RelatedNotice key={`${notice.type}-${notice.source}-${index}`} notice={notice} />)}</div> : null}
    {items.length ? <div className="relationship-list">{items.map((item, index) => <RelatedResourceRow key={`${item.type}-${item.source}-${item.namespace || ''}-${item.name || ''}-${item.uid || index}`} item={item} onOpenResource={onOpenResource} />)}</div> : notices.length ? <div className="relationship-limited"><AlertTriangle size={16} /><span>No resolved links are visible. The source states above explain what Kubernetes allowed this identity to read.</span></div> : <QuietState icon={<Network size={22} />} title="No related resources" detail={displayedResponse?.message || 'No safe native relationship was found for this object.'} />}
  </div>
}

/** Merges direct and deferred relationship responses without retaining the deferred placeholder. */
function mergeRelatedResponses(initial: RelatedResourcesResponse, secondary: RelatedResourcesResponse): RelatedResourcesResponse {
  const items = [...initial.items]
  const itemKeys = new Set(items.map((item) => [item.type, item.source, item.apiVersion || '', item.kind || '', item.namespace || '', item.name || '', item.uid || ''].join('\u0000')))
  secondary.items.forEach((item) => {
    const key = [item.type, item.source, item.apiVersion || '', item.kind || '', item.namespace || '', item.name || '', item.uid || ''].join('\u0000')
    if (itemKeys.has(key)) return
    itemKeys.add(key)
    items.push(item)
  })

  const notices = [...(initial.notices || []).filter((notice) => !notice.deferred), ...(secondary.notices || [])]
  const noticeKeys = new Set<string>()
  const uniqueNotices = notices.filter((notice) => {
    const key = [notice.type, notice.source, notice.state, notice.message].join('\u0000')
    if (noticeKeys.has(key)) return false
    noticeKeys.add(key)
    return true
  })
  const truncated = Boolean(initial.truncated || secondary.truncated)
  let state = initial.state
  if (truncated) {
    state = 'partial'
  } else if (secondary.state === 'permission-denied' && !items.length) {
    state = 'permission-denied'
  } else if (secondary.state !== 'error' && (secondary.state === 'available' || secondary.state === 'empty') && uniqueNotices.length === 0) {
    state = items.length ? 'available' : 'empty'
  } else if (items.length || uniqueNotices.length) {
    state = 'partial'
  }

  const message = state === 'available'
    ? 'Relationships resolved from Kubernetes metadata and native spec fields.'
    : state === 'empty'
      ? 'No native relationships were found for this resource.'
      : state === 'partial'
        ? 'Some relationship sources were incomplete; each limitation is listed below.'
        : initial.message || secondary.message

  const diagnostics = initial.diagnostics && secondary.diagnostics ? {
    queryCount: initial.diagnostics.queryCount + secondary.diagnostics.queryCount,
    queryBudget: Math.max(initial.diagnostics.queryBudget, secondary.diagnostics.queryBudget),
    durationMilliseconds: initial.diagnostics.durationMilliseconds + secondary.diagnostics.durationMilliseconds,
    deadlineMilliseconds: Math.max(initial.diagnostics.deadlineMilliseconds, secondary.diagnostics.deadlineMilliseconds),
    concurrentLimit: Math.max(initial.diagnostics.concurrentLimit, secondary.diagnostics.concurrentLimit),
  } : secondary.diagnostics || initial.diagnostics
  return { ...initial, state, message, items, notices: uniqueNotices, truncated, diagnostics }
}

/** Describes one action option shown by a targeted workload form. */
type WorkloadActionOption = {
  action: WorkloadAction
  label: string
  detail: string
}

/** Renders a two-step dry-run and confirmation form for built-in workloads. */
function WorkloadActionsPanel({ context, namespace, definition, name, object, writeEnabled, reloadEpoch, onRefresh }: { context: string; namespace: string; definition: ResourceDefinition; name: string; object: ResourceObject; writeEnabled: boolean; reloadEpoch: number; onRefresh: () => Promise<void> }) {
  const options = workloadActionOptions(definition.resource)
  const [action, setAction] = useState<WorkloadAction>(options[0]?.action || 'restart')
  const [replicas, setReplicas] = useState(workloadReplicaInput(object))
  const [container, setContainer] = useState(workloadContainerNames(definition.resource, object)[0] || '')
  const [image, setImage] = useState(workloadContainerImage(definition.resource, object, container))
  const [revision, setRevision] = useState('')
  const [schedule, setSchedule] = useState(workloadSchedule(object))
  const [review, setReview] = useState<WorkloadActionResponse | null>(null)
  const [reviewToken, setReviewToken] = useState<string | undefined>()
  const [confirmed, setConfirmed] = useState(false)
  const [busy, setBusy] = useState<'review' | 'apply' | ''>('')
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const containers = workloadContainerNames(definition.resource, object)
  const selectedOption = options.find((option) => option.action === action) || options[0]
  const parsedReplicas = parseWorkloadReplicaInput(replicas)
  const scaleInputInvalid = action === 'scale' && parsedReplicas === undefined

  useEffect(() => {
    const nextOptions = workloadActionOptions(definition.resource)
    if (!nextOptions.some((option) => option.action === action)) setAction(nextOptions[0]?.action || 'restart')
  }, [definition.resource])

  useEffect(() => {
    const nextContainers = workloadContainerNames(definition.resource, object)
    const nextContainer = nextContainers.includes(container) ? container : nextContainers[0] || ''
    setContainer(nextContainer)
    setImage(workloadContainerImage(definition.resource, object, nextContainer))
    setReplicas(workloadReplicaInput(object))
    setSchedule(workloadSchedule(object))
    setReview(null)
    setReviewToken(undefined)
    setConfirmed(false)
  }, [object])

  useEffect(() => {
    setReview(null)
    setReviewToken(undefined)
    setConfirmed(false)
  }, [context, writeEnabled, reloadEpoch])

  /** Clears a review so the operator can change the requested action. */
  function editAction() {
    setReview(null)
    setReviewToken(undefined)
    setConfirmed(false)
    setError('')
  }

  /** Changes the selected workload action and invalidates an older dry-run review. */
  function changeAction(value: WorkloadAction) {
    setAction(value)
    setReview(null)
    setReviewToken(undefined)
    setConfirmed(false)
    setError('')
    setSuccess('')
  }

  /** Builds and validates the explicit payload sent to the workload action endpoint. */
  function buildWorkloadRequest(dryRun: boolean): WorkloadActionRequest | null {
    const expectedResourceVersion = metadataString(object, 'resourceVersion')
    const request: WorkloadActionRequest = { action, dryRun, expectedResourceVersion }
    if (action === 'scale') {
      const validatedReplicas = parseWorkloadReplicaInput(replicas)
      if (validatedReplicas === undefined) {
        setError(WORKLOAD_REPLICA_VALIDATION_MESSAGE)
        return null
      }
      request.replicas = validatedReplicas
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
       const response = await applyWorkloadAction(context, definition, namespace, name, request)
       if (!response.reviewToken) {
         setReview(null)
         setReviewToken(undefined)
         setError('The server did not return a mutation review handle. Review the action again.')
         return
       }
       setReview(response.value)
       setReviewToken(response.reviewToken)
      setConfirmed(false)
    } catch (reviewError) {
      setReview(null)
      setReviewToken(undefined)
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
    if (!review || !reviewToken) {
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
       const response = await applyWorkloadAction(context, definition, namespace, name, request, reviewToken)
       const result = response.value
      const created = result.createdName ? ` / created ${result.createdName}` : ''
      setSuccess(`${workloadActionLabel(action)} applied${created}.`)
       setReview(null)
       setReviewToken(undefined)
      setConfirmed(false)
      try {
        await onRefresh()
      } catch (refreshError) {
        setError(`Action applied, but the workload refresh failed: ${(refreshError as Error).message}`)
      }
    } catch (applyError) {
      setReview(null)
      setReviewToken(undefined)
      setConfirmed(false)
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
      {action === 'scale' && <label className="workload-operation-field"><span>Desired replicas</span><input type="text" inputMode="numeric" value={replicas} onChange={(event) => setReplicas(event.target.value)} disabled={disabled} aria-invalid={scaleInputInvalid} aria-describedby={scaleInputInvalid ? 'workload-replica-guidance' : undefined} />{scaleInputInvalid && <small id="workload-replica-guidance" className="workload-field-error">{WORKLOAD_REPLICA_VALIDATION_MESSAGE}</small>}</label>}
      {action === 'image' && <div className="workload-operation-grid"><label className="workload-operation-field"><span>Container</span><select value={container} onChange={(event) => { setContainer(event.target.value); setImage(workloadContainerImage(definition.resource, object, event.target.value)) }} disabled={disabled}><option value="">Choose container</option>{containers.map((item) => <option key={item} value={item}>{item}</option>)}</select></label><label className="workload-operation-field"><span>New image</span><input value={image} onChange={(event) => setImage(event.target.value)} disabled={disabled} placeholder="registry.example/app:tag" /></label></div>}
      {action === 'rollback' && <label className="workload-operation-field"><span>Revision <em>blank = previous</em></span><input type="number" min="1" step="1" value={revision} onChange={(event) => setRevision(event.target.value)} disabled={disabled} placeholder="previous" /></label>}
      {action === 'schedule' && <label className="workload-operation-field"><span>Cron schedule</span><input value={schedule} onChange={(event) => setSchedule(event.target.value)} disabled={disabled} placeholder="*/15 * * * *" /></label>}
      <div className="workload-operation-actions"><span>{selectedOption?.detail}</span><button type="submit" className="primary-button" disabled={!writeEnabled || Boolean(busy) || Boolean(review) || scaleInputInvalid}>{busy === 'review' ? <LoaderCircle size={15} className="spin" /> : <ShieldCheck size={15} />} {busy === 'review' ? 'Reviewing' : 'Review action'}</button></div>
    </form>
    {review && <div className="workload-review"><div className="workload-review-heading"><span className="review-ready"><Check size={13} /> Dry-run passed</span><strong>{workloadActionLabel(action)} is ready</strong></div><p>Kubernetes accepted this exact resource-versioned operation for server-side dry-run. No cluster state has changed; the final request can still change live workload behavior.</p><label className="workload-confirm"><input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} disabled={busy === 'apply'} /><span>I reviewed the exact action and authorize the final request to change workload state.</span></label><div className="workload-review-actions"><button type="button" className="secondary-button" onClick={editAction} disabled={Boolean(busy)}>Change action</button><button type="button" className="primary-button" onClick={() => void applyReviewedAction()} disabled={busy === 'apply' || !confirmed}>{busy === 'apply' ? <LoaderCircle size={15} className="spin" /> : <Check size={15} />} {busy === 'apply' ? 'Applying' : 'Confirm and apply'}</button></div></div>}
    {error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}
    {success && <div className="operation-success"><Check size={15} />{success}</div>}
    <div className="workload-operation-footer"><span><ShieldCheck size={14} /> Native dynamic update / subresource</span><span>RBAC denials, conflicts, and unsupported APIs stay visible.</span></div>
  </div>
}

/** Renders a two-step PVC resize and VolumeSnapshot workflow with capability states. */
function PVCOperationsPanel({ context, namespace, name, object, writeEnabled, reloadEpoch, onRefresh }: { context: string; namespace: string; name: string; object: ResourceObject; writeEnabled: boolean; reloadEpoch: number; onRefresh: () => Promise<void> }) {
  const [info, setInfo] = useState<PVCOperationInfo | null>(null)
  const [action, setAction] = useState<PVCAction>('resize')
  const [storage, setStorage] = useState(pvcRequestedStorage(object))
  const [snapshotName, setSnapshotName] = useState('')
  const [snapshotClassName, setSnapshotClassName] = useState('')
  const [review, setReview] = useState<PVCActionResponse | null>(null)
  const [reviewToken, setReviewToken] = useState<string | undefined>()
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
    setReviewToken(undefined)
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

  useEffect(() => {
    setReview(null)
    setReviewToken(undefined)
    setConfirmed(false)
  }, [context, writeEnabled, reloadEpoch])

  /** Clears an older review when the operator changes the native PVC operation. */
  function changeAction(value: PVCAction) {
    setAction(value)
    setReview(null)
    setReviewToken(undefined)
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
       const response = await applyPVCAction(context, namespace, name, request)
       if (!response.reviewToken) {
         setReview(null)
         setReviewToken(undefined)
         setError('The server did not return a mutation review handle. Review the operation again.')
         return
       }
       setReview(response.value)
       setReviewToken(response.reviewToken)
      setConfirmed(false)
    } catch (reviewError) {
      setReview(null)
      setReviewToken(undefined)
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
    if (!review || !reviewToken) {
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
       const response = await applyPVCAction(context, namespace, name, request, reviewToken)
       const result = response.value
      setSuccess(action === 'resize' ? 'PVC resize applied. Kubernetes will complete expansion asynchronously.' : `VolumeSnapshot ${result.snapshotName || snapshotName.trim()} creation requested.`)
       setReview(null)
       setReviewToken(undefined)
      setConfirmed(false)
      try {
        await onRefresh()
      } catch (refreshError) {
        setError(`PVC operation applied, but the PVC refresh failed: ${(refreshError as Error).message}`)
      }
    } catch (applyError) {
      setReview(null)
      setReviewToken(undefined)
      setConfirmed(false)
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
     {review && <div className="pvc-review"><div className="pvc-review-heading"><span className="review-ready"><Check size={13} /> Dry-run passed</span><strong>{action === 'resize' ? 'Resize is ready' : 'Snapshot is ready'}</strong></div><p>Kubernetes accepted this exact resource-versioned request during server-side dry-run. No cluster state has changed; the final request is separate and must be explicitly authorized.</p><label className="pvc-confirm"><input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} disabled={busy === 'apply'} /><span>I reviewed the exact dry-run and explicitly authorize this {action === 'resize' ? 'PVC resize' : 'VolumeSnapshot creation'}.</span></label><div className="pvc-review-actions"><button type="button" className="secondary-button" onClick={() => { setReview(null); setReviewToken(undefined); setConfirmed(false); setError('') }} disabled={Boolean(busy)}>Change operation</button><button type="button" className="primary-button" onClick={() => void applyReviewedPVCAction()} disabled={busy === 'apply' || !confirmed}>{busy === 'apply' ? <LoaderCircle size={15} className="spin" /> : <Check size={15} />} {busy === 'apply' ? 'Applying' : 'Confirm and apply'}</button></div></div>}
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
function NodeOperationsPanel({ context, name, object, writeEnabled, reloadEpoch, onRefresh }: { context: string; name: string; object: ResourceObject; writeEnabled: boolean; reloadEpoch: number; onRefresh: () => Promise<void> }) {
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
  const [reviewToken, setReviewToken] = useState<string | undefined>()
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
    setReviewToken(undefined)
    setConfirmed(false)
    setConfirmationText('')
  }, [object])

  useEffect(() => {
    setReview(null)
    setReviewToken(undefined)
    setConfirmed(false)
    setConfirmationText('')
  }, [context, writeEnabled, reloadEpoch])

  /** Clears a review so the operator can change the Node operation. */
  function editNodeAction() {
    setReview(null)
    setReviewToken(undefined)
    setConfirmed(false)
    setConfirmationText('')
    setError('')
  }

  /** Changes the selected Node operation and invalidates an older dry-run review. */
  function changeNodeAction(value: NodeAction) {
    setAction(value)
    setReview(null)
    setReviewToken(undefined)
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
       const response = await applyNodeAction(context, name, request)
       if (!response.reviewToken) {
         setReview(null)
         setReviewToken(undefined)
         setError('The server did not return a mutation review handle. Review the operation again.')
         return
       }
       setReview(response.value)
       setReviewToken(response.reviewToken)
      setConfirmed(false)
      setConfirmationText('')
    } catch (reviewError) {
      setReview(null)
      setReviewToken(undefined)
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
    if (!review || !reviewToken || !review.ready || review.status !== 'dry-run passed') {
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
       const response = await applyNodeAction(context, name, request, reviewToken)
       const result = response.value
      const drainSummary = action === 'drain' ? ` ${result.evictedCount || 0} Pod(s) evicted; ${result.skippedCount || 0} left in place.` : ''
      setSuccess(`${nodeActionLabel(action)} applied.${drainSummary}`)
       setReview(null)
       setReviewToken(undefined)
      setConfirmed(false)
      setConfirmationText('')
      try {
        await onRefresh()
      } catch (refreshError) {
        setError(`Node operation applied, but the Node refresh failed: ${(refreshError as Error).message}`)
      }
    } catch (applyError) {
      setReview(null)
      setReviewToken(undefined)
      setConfirmed(false)
      setConfirmationText('')
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
  const targetPath = relatedResourceDetailPath(item)
  const canOpen = Boolean(targetPath)
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
    'involved-object': 'Event involved object',
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
        if (!response.ok) throw await responseApiError(response, `Log request failed with ${response.status}`)
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
        if ((loadError as Error).name !== 'AbortError') setError((loadError as Error).name === 'ApiError' ? (loadError as Error).message : 'The local API could not be reached (retry: retry)')
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
      if (message.type === 'error') setError(streamFailureMessage(message, 'The terminal session failed.'))
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
        setError(streamFailureMessage(message, 'The port-forward session failed.'))
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
function ResourceEventsPanel({ context, namespace, uid, revision, reloadEpoch, onOpenEvent }: { context: string; namespace: string; uid: string; revision: number; reloadEpoch: number; onOpenEvent: (event: EventSummary) => void }) {
  const [events, setEvents] = useState<EventSummary[]>([])
  const [error, setError] = useState('')
  const [watchSnapshot, setWatchSnapshot] = useState<WatchSnapshot>(() => initialWatchSnapshot())
  const [loading, setLoading] = useState(Boolean(uid))

  useEffect(() => {
    if (!uid) {
      setLoading(false)
      return
    }
    setWatchSnapshot(initialWatchSnapshot())
    setError('')
    const effectiveNamespace = namespace === 'all' ? '' : namespace
    const controller = new ReliableWatchController<ResourceEventsResponse>({
      load: (signal) => fetchResourceEvents(context, effectiveNamespace, uid, signal),
      getResourceVersion: (data) => data.resourceVersion,
      watchUrl: (resourceVersion) => resourceEventsWatchUrl(context, effectiveNamespace, uid, resourceVersion),
      onData: (response) => {
        setEvents(response.items)
        setError('')
        setLoading(false)
      },
      onState: (snapshot) => {
        setWatchSnapshot(snapshot)
        setLoading(snapshot.state === 'refreshing')
        if (snapshot.state === 'stale' || snapshot.state === 'unavailable') setError(snapshot.message || '')
      },
    })
    controller.start()
    return () => controller.stop()
  }, [context, namespace, uid, revision, reloadEpoch])

  /** Opens a generic Event detail page only when Kubernetes supplied its name. */
  function openEvent(event: EventSummary) {
    if (!event.name) {
      setError('This Event has no Kubernetes name. Refresh the resource and try again.')
      return
    }
    onOpenEvent(event)
  }

  if (!uid) return <QuietState icon={<Activity size={22} />} title="Events need a resource UID" detail="This object does not expose a UID that can be used for native Event filtering." />

  return <div className="events-panel"><div className="events-toolbar"><div className="logs-title"><Activity size={16} /><strong>Resource Events</strong><span>{events.length} recorded</span></div><FreshnessStatus snapshot={watchSnapshot} /></div><FreshnessNotice snapshot={watchSnapshot} />{error && <div className="inline-error page-error"><AlertTriangle size={15} />{error}</div>}{loading && !events.length ? <div className="events-loading"><LoaderCircle size={17} className="spin" /> Reading resource Events</div> : null}{!loading && !events.length && !error ? <QuietState icon={<ShieldCheck size={22} />} title="No Events for this resource" detail="Kubernetes has not reported an Event associated with this object." /> : null}<div className="event-list">{events.map((event, index) => <button type="button" className="event-row event-row-button" key={`${event.name}-${event.reason}-${event.lastSeen}-${index}`} onClick={() => openEvent(event)}><span className={`event-type event-type-${event.type.toLowerCase()}`}><span />{event.type || 'Normal'}</span><span className="event-copy"><strong>{event.reason || 'Event'}</strong><span>{event.message || 'No message provided.'}</span></span><span className="event-meta"><code>{event.count > 1 ? `x${event.count}` : 'once'}</code><span>{relativeTime(event.lastSeen)}</span></span></button>)}</div></div>
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
  return parseResourceRoute(window.location.pathname, window.location.search)
}

/** Reads a string from a resource metadata object. */
function metadataString(object: ResourceObject, field: string): string {
  const metadata = object.metadata
  if (!metadata || typeof metadata !== 'object' || Array.isArray(metadata)) return ''
  const value = (metadata as Record<string, unknown>)[field]
  return typeof value === 'string' ? value : ''
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

/** Bounds a read-only conflict snapshot so a large generic resource does not dominate the editor. */
function boundedConflictText(value: string): string {
  const maximum = 12000
  if (value.length <= maximum) return value
  return `${value.slice(0, maximum)}\n# Snapshot truncated in the conflict review; reload remains explicit.`
}

/** Converts a resource name into a safe filename fragment for local draft export. */
function safeDownloadName(value: string): string {
  const name = value.trim().replace(/[^a-zA-Z0-9._-]+/g, '-')
  return name || 'resource'
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
