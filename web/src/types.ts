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
  sourceState: KubeconfigSourceState
  revision: number
  reloaded: boolean
  error?: string
  reloadError?: string
}

/** Describes one redacted local mutation outcome returned by the audit API. */
export interface AuditEntry {
  id: number
  createdAt: string
  context: string
  operation: string
  group: string
  version: string
  resource: string
  namespace?: string
  name?: string
  dryRun: boolean
  success: boolean
  detail?: string
}

/** Contains the bounded newest slice of local mutation history. */
export interface AuditResponse {
  items: AuditEntry[]
}

/** Identifies the safe state of the backend kubeconfig source snapshot. */
export type KubeconfigSourceState = 'ready' | 'changed' | 'error'

/** Describes one resource discovered from a Kubernetes API server. */
export interface ResourceDefinition {
  group: string
  version: string
  resource: string
  kind: string
  namespaced: boolean
  customResource?: boolean
  verbs: string[]
  shortNames?: string[]
  categories?: string[]
}

/** Identifies why a schema form is available or why YAML remains the fallback. */
export type ResourceSchemaState = 'available' | 'unsupported' | 'missing' | 'non-structural' | 'permission-denied' | 'error'

/** Describes one normalized structural OpenAPI field used by the browser form. */
export interface ResourceSchemaField {
  name?: string
  type: 'string' | 'integer' | 'number' | 'boolean' | 'object' | 'array'
  title?: string
  description?: string
  format?: string
  required?: boolean
  readOnly?: boolean
  enum?: unknown[]
  default?: unknown
  properties?: ResourceSchemaField[]
  items?: ResourceSchemaField
  additionalProperties?: ResourceSchemaField
  minimum?: number
  exclusiveMinimum?: boolean
  maximum?: number
  exclusiveMaximum?: boolean
  multipleOf?: number
  minLength?: number
  maxLength?: number
  pattern?: string
  minItems?: number
  maxItems?: number
  uniqueItems?: boolean
}

/** Contains the read-only structural CRD schema and explicit YAML fallback state. */
export interface ResourceSchemaResponse {
  state: ResourceSchemaState
  message?: string
  group: string
  version: string
  resource: string
  kind?: string
  schema?: ResourceSchemaField
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

/** Describes one safe, resource-versioned selection in a generic bulk delete. */
export interface ResourceDeleteItem {
  name: string
  namespace?: string
  resourceVersion: string
}

/** Contains the redacted result of one generic resource deletion. */
export interface ResourceDeleteResponse {
  name: string
  namespace?: string
  dryRun: boolean
  status: string
}

/** Reports one generic bulk-delete result without returning resource bodies. */
export interface BulkDeleteItemResult {
  name: string
  namespace?: string
  success: boolean
  status: string
  error?: string
}

/** Contains bulk-delete counts and bounded per-item results. */
export interface BulkDeleteResponse {
  resource: string
  namespace?: string
  dryRun: boolean
  total: number
  succeeded: number
  failed: number
  status: string
  items: BulkDeleteItemResult[]
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

/** Describes one targeted native operation request for a built-in workload. */
export interface WorkloadActionRequest {
  action: WorkloadAction
  dryRun: boolean
  expectedResourceVersion?: string
  replicas?: number
  container?: string
  image?: string
  revision?: number
  schedule?: string
}

/** Identifies the workload actions exposed by the targeted detail form. */
export type WorkloadAction = 'scale' | 'restart' | 'pause' | 'resume' | 'rollback' | 'image' | 'schedule' | 'run-now'

/** Contains the redacted outcome of one targeted workload operation. */
export interface WorkloadActionResponse {
  action: WorkloadAction
  resource: string
  namespace: string
  name: string
  dryRun: boolean
  resourceVersion?: string
  createdName?: string
  status: string
}

/** Identifies the explicit capability states returned for native PVC operations. */
export type PVCOperationState = 'available' | 'unsupported' | 'unavailable' | 'permission-denied' | 'error' | 'empty'

/** Describes whether the PVC StorageClass permits a larger storage request. */
export interface PVCResizeCapability {
  state: PVCOperationState
  message?: string
  storageClassName?: string
  currentStorage?: string
  allowVolumeExpansion: boolean
}

/** Describes one non-secret VolumeSnapshotClass choice returned by discovery. */
export interface VolumeSnapshotClass {
  name: string
  driver?: string
  deletionPolicy?: string
}

/** Describes snapshot API discovery and the readable classes available for one PVC. */
export interface VolumeSnapshotCapability {
  state: PVCOperationState
  message?: string
  apiGroupVersion?: string
  classes: VolumeSnapshotClass[]
}

/** Contains the read-only capability and concurrency data used by the PVC Operations UI. */
export interface PVCOperationInfo {
  name: string
  namespace: string
  resourceVersion: string
  requestedStorage?: string
  storageClassName?: string
  resize: PVCResizeCapability
  snapshot: VolumeSnapshotCapability
}

/** Identifies the native PVC operation exposed by the focused detail form. */
export type PVCAction = 'resize' | 'snapshot'

/** Describes one explicit PVC resize or VolumeSnapshot request. */
export interface PVCActionRequest {
  action: PVCAction
  dryRun: boolean
  confirmed?: boolean
  expectedResourceVersion?: string
  storage?: string
  snapshotName?: string
  snapshotClassName?: string
}

/** Contains the redacted result of one native PVC storage operation. */
export interface PVCActionResponse {
  action: PVCAction
  resource: string
  namespace: string
  name: string
  dryRun: boolean
  resourceVersion?: string
  snapshotName?: string
  snapshotClassName?: string
  status: string
}

/** Describes one explicit native operation request for a cluster-scoped Node. */
export interface NodeActionRequest {
  action: NodeAction
  dryRun: boolean
  expectedResourceVersion?: string
  taintKey?: string
  taintValue?: string
  taintEffect?: string
  deleteEmptyDirData?: boolean
  ignoreDaemonSets?: boolean
  gracePeriodSeconds?: number
}

/** Identifies the supported native operations exposed by the Node detail view. */
export type NodeAction = 'cordon' | 'uncordon' | 'drain' | 'add-taint' | 'remove-taint'

/** Contains one safe, non-secret Node taint summary. */
export interface NodeTaint {
  key: string
  value?: string
  effect: string
}

/** Describes one Pod's impact or limitation during a Node drain review. */
export interface NodePodImpact {
  namespace: string
  name: string
  phase: string
  controller?: string
  action: 'evict' | 'skipped' | 'blocked'
  usesEmptyDir?: boolean
  limitation?: string
}

/** Contains the redacted result of one native Node operation or drain review. */
export interface NodeActionResponse {
  action: NodeAction
  resource: string
  name: string
  dryRun: boolean
  ready: boolean
  resourceVersion?: string
  status: string
  taints?: NodeTaint[]
  pods?: NodePodImpact[]
  podCount?: number
  evictedCount?: number
  skippedCount?: number
  blockedCount?: number
  limitations?: string[]
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
  view?: 'history'
  resource?: string
  name?: string
  namespace?: string
}
