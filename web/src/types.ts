/** Describes one kubeconfig context returned by the backend. */
export interface ContextSummary {
  name: string
  cluster: string
  user: string
  server: string
  isCurrent: boolean
  reachable: boolean
  lastError?: string
}

/** Contains the contexts available to the local KubeHaggis process. */
export interface ContextsResponse {
  currentContext: string
  contexts: ContextSummary[]
  error?: string
}

/** Describes one resource discovered from a Kubernetes API server. */
export interface ResourceDefinition {
  group: string
  version: string
  resource: string
  kind: string
  namespaced: boolean
  verbs: string[]
  shortNames?: string[]
  categories?: string[]
}

/** Contains common resource fields used by the generic resource table. */
export interface ResourceSummary {
  name: string
  namespace?: string
  kind: string
  uid: string
  createdAt: string
  status: string
  ready?: string
  labels?: Record<string, string>
  resourceVersion: string
}

/** Contains a generic resource collection response. */
export interface ResourceListResponse {
  definition: ResourceDefinition
  items: ResourceSummary[]
  continue?: string
  resourceVersion: string
}

/** Contains node counters shown on the cluster overview. */
export interface NodeOverview {
  total: number
  ready: number
  notReady: number
  cordoned: number
  pressure: number
}

/** Contains workload counters shown on the cluster overview. */
export interface WorkloadOverview {
  deployments: number
  healthyDeployments: number
  statefulSets: number
  healthyStatefulSets: number
  daemonSets: number
  healthyDaemonSets: number
  jobs: number
  failedJobs: number
  cronJobs: number
}

/** Contains Pod phase counters shown on the cluster overview. */
export interface PodOverview {
  total: number
  running: number
  pending: number
  failed: number
  restarting: number
}

/** Contains PersistentVolumeClaim counters shown on the cluster overview. */
export interface StorageOverview {
  total: number
  bound: number
  pending: number
  lost: number
}

/** Identifies the explicit state of a native metrics source or aggregate. */
export type MetricsState = 'available' | 'unavailable' | 'empty' | 'permission-denied' | 'error' | 'stale'

/** Contains normalized CPU and memory values returned by metrics.k8s.io. */
export interface MetricUsage {
  cpuCores: number
  memoryBytes: number
}

/** Contains one node's usage and capacity utilization values. */
export interface NodeMetric {
  name: string
  usage: MetricUsage
  cpuPercent: number
  memoryPercent: number
}

/** Contains one Pod's usage aggregated across its containers. */
export interface PodMetric {
  name: string
  namespace?: string
  usage: MetricUsage
}

/** Contains native metrics data and explicit node/Pod source states. */
export interface MetricsOverview {
  state: MetricsState
  message?: string
  updatedAt?: string
  nodeState: MetricsState
  nodeMessage?: string
  nodeUpdatedAt?: string
  nodeCount: number
  nodes: NodeMetric[]
  podState: MetricsState
  podMessage?: string
  podUpdatedAt?: string
  podCount: number
  pods: PodMetric[]
}

/** Contains a compact warning Event shown on the cluster overview. */
export interface EventSummary {
  type: string
  reason: string
  message: string
  name: string
  namespace?: string
  object: string
  count: number
  firstSeen: string
  lastSeen: string
}

/** Contains Events associated with one Kubernetes resource. */
export interface ResourceEventsResponse {
  items: EventSummary[]
  resourceVersion: string
}

/** Contains the aggregate data used by the cluster landing page. */
export interface ClusterOverview {
  context: ContextSummary
  namespaces: number
  nodes: NodeOverview
  workloads: WorkloadOverview
  pods: PodOverview
  storage: StorageOverview
  warnings: EventSummary[]
  metricsAvailable: boolean
  metrics: MetricsOverview
  updatedAt: string
}

/** Represents an unstructured Kubernetes resource object. */
export type ResourceObject = Record<string, unknown>

/** Contains the supported fields for one ephemeral debug-container launch. */
export interface EphemeralContainerRequest {
  name: string
  image: string
  command: string[]
  targetContainerName: string
}

/** Contains the minimal acknowledgement returned after a Pod operation. */
export interface PodOperationResponse {
  status: string
  pod: string
  container?: string
}

/** Describes the standard Kubernetes owner reference attached to a resource. */
export interface ResourceOwnerReference {
  apiVersion: string
  kind: string
  name: string
  uid?: string
  controller?: boolean
  blockOwnerDeletion?: boolean
}

/** Identifies the aggregate state of a related-resource response. */
export type RelatedAggregateState = 'available' | 'empty' | 'partial' | 'permission-denied' | 'error' | 'unavailable'

/** Identifies the state of one relationship target or source notice. */
export type RelatedItemState = 'resolved' | 'unresolved' | 'permission-denied' | 'error' | 'unavailable' | 'partial'

/** Describes one sanitized related-resource target or explicit unresolved relationship. */
export interface RelatedResource {
  type: string
  source: string
  state: RelatedItemState
  definition?: ResourceDefinition
  apiVersion?: string
  kind?: string
  name?: string
  namespace?: string
  uid?: string
  message?: string
  controller?: boolean
  blockOwnerDeletion?: boolean
}

/** Describes a relationship source that could not be fully read. */
export interface RelatedRelationshipNotice {
  type: string
  source: string
  state: RelatedItemState
  message: string
}

/** Contains bounded related-resource targets, source notices, and aggregate state. */
export interface RelatedResourcesResponse {
  state: RelatedAggregateState
  message?: string
  items: RelatedResource[]
  notices?: RelatedRelationshipNotice[]
  truncated?: boolean
}

/** Represents one browser message exchanged with a native Pod terminal. */
export interface TerminalMessage {
  type: 'input' | 'resize' | 'output' | 'error' | 'closed'
  data?: string
  message?: string
  rows?: number
  cols?: number
}

/** Represents the text control messages exchanged around a binary port-forward stream. */
export interface PortForwardMessage {
  type: 'ready' | 'error' | 'closed'
  message?: string
  targetPort?: number
  localPort?: number
}

/** Represents the browser route used by the single-page application. */
export interface RouteState {
  path: string
  resource?: string
  name?: string
  namespace?: string
}
