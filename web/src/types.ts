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

/** Identifies the safe retry guidance attached to an API failure. */
export type RetryClassification = 'retry' | 'check-and-retry' | 'refresh-and-review'

/** Describes the browser-safe kubeconfig source summary in local diagnostics. */
export interface DiagnosticsKubeconfig {
  source: string
  state: string
  revision: number
}

/** Describes the selected kubeconfig context without cluster credentials. */
export interface DiagnosticsContext {
  current: string
  selected: string
  state: string
}

/** Describes safe revision-scoped discovery-cache age and aggregate state. */
export interface DiagnosticsDiscovery {
  context: string
  revision: number
  state: string
  ageSeconds?: number
  loadedAt?: string
  itemCount: number
  issueCount: number
}

/** Describes one safe server-side watch lifecycle transition. */
export interface DiagnosticsWatchEvent {
  at: string
  context?: string
  resource?: string
  state: string
  reconnect?: boolean
  errorCategory?: string
}

/** Describes the latest safe state for one context and resource watch. */
export interface DiagnosticsWatchView {
  context?: string
  resource?: string
  state: string
  lastAt: string
  errorCategory?: string
  reconnects: number
  active: boolean
}

/** Contains bounded current and recent server-side watch lifecycle information. */
export interface DiagnosticsWatches {
  active: number
  current: DiagnosticsWatchView[]
  recent: DiagnosticsWatchEvent[]
}

/** Contains active native terminal, log, port-forward, and watch counts. */
export interface DiagnosticsSessions {
  terminal: number
  logs: number
  portForward: number
  watch: number
}

/** Contains safe local audit-store availability. */
export interface DiagnosticsAudit {
  state: string
}

/** Contains bounded request counters, status totals, operations, and timings. */
export interface DiagnosticsRequests {
  total: number
  errors: number
  averageDurationMilliseconds: number
  maxDurationMilliseconds: number
  statuses: Record<string, number>
  operations: Record<string, number>
  errorCategories: Record<string, number>
}

/** Describes one recent safe backend error category without its original message. */
export interface DiagnosticsErrorEvent {
  at: string
  status?: number
  category: string
  operation: string
}

/** Contains all bounded local diagnostics exposed by the diagnostics endpoint. */
export interface DiagnosticsResponse {
  generatedAt: string
  uptimeSeconds: number
  kubeconfig: DiagnosticsKubeconfig
  context: DiagnosticsContext
  discovery: DiagnosticsDiscovery
  watches: DiagnosticsWatches
  recentErrors: DiagnosticsErrorEvent[]
  sessions: DiagnosticsSessions
  audit: DiagnosticsAudit
  requests: DiagnosticsRequests
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
  scope: ResourceScope
  customResource?: boolean
  verbs: string[]
  shortNames?: string[]
  categories?: string[]
  subresources?: ResourceSubresource[]
  customResourceCapabilities?: CustomResourceCapabilities
}

/** Describes bounded CRD metadata attached to one served custom-resource GVR. */
export interface CustomResourceCapabilities {
  group: string
  version: string
  kind: string
  plural: string
  resource: string
  scope: ResourceScope | string
  servedVersions: string[]
  preferredVersion: string
  storageVersion: string
  statusSubresource: boolean
  scaleSubresource: boolean
  scaleSpecReplicasPath?: string
  scaleStatusReplicasPath?: string
  scaleLabelSelectorPath?: string
  printerColumns?: CustomResourcePrinterColumn[]
  printerColumnWarnings?: string[]
}

/** Describes one safe, tokenized CRD printer column exposed to custom-resource tables. */
export interface CustomResourcePrinterColumn {
  name: string
  type: string
  description?: string
  priority?: number
  path: string[]
}

/** Identifies the namespace scope reported by Kubernetes discovery. */
export type ResourceScope = 'Namespaced' | 'Cluster'

/** Describes one discovered subresource capability attached to a top-level resource. */
export interface ResourceSubresource {
  name: string
  verbs: string[]
}

/** Identifies the aggregate health of the discovery-backed catalog. */
export type ResourceCatalogState = 'available' | 'partial' | 'unavailable' | 'permission-denied' | 'error'

/** Describes one API group or version that could not be completely discovered. */
export interface DiscoveryIssue {
  group?: string
  version?: string
  state: Exclude<ResourceCatalogState, 'available' | 'partial'>
  message: string
  retryable: boolean
}

/** Contains the complete resource catalog and explicit discovery issue state. */
export interface ResourceCatalogResponse {
  items: ResourceDefinition[]
  state: ResourceCatalogState
  issues?: DiscoveryIssue[]
}

/** Identifies why a schema form is available or why YAML remains the fallback. */
export type ResourceSchemaState = 'available' | 'unsupported' | 'missing' | 'non-structural' | 'permission-denied' | 'unavailable' | 'malformed' | 'error'

/** Describes one normalized structural OpenAPI field used by the browser form. */
export interface ResourceSchemaField {
  name?: string
  type: 'string' | 'integer' | 'number' | 'boolean' | 'object' | 'array'
  scalarTypes?: Array<'string' | 'integer' | 'number'>
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

/** Contains the normalized resource schema and explicit YAML fallback state. */
export interface ResourceSchemaResponse {
  state: ResourceSchemaState
  message?: string
  group: string
  version: string
  resource: string
  kind?: string
  source?: 'openapi-v3' | 'openapi-v2' | 'crd'
  schema?: ResourceSchemaField
  fallbacks?: ResourceSchemaFallback[]
  fallbacksTruncated?: boolean
  statusSubresource?: boolean
  scaleStatusPath?: string
}

/** Explains one schema field that remains available through the complete YAML editor. */
export interface ResourceSchemaFallback {
  path: string
  reason: string
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
  printerColumns?: Record<string, string>
}

/** Contains a generic resource collection response. */
export interface ResourceListResponse {
  definition: ResourceDefinition
  items: ResourceSummary[]
  continue?: string
  resourceVersion: string
}

/** Contains the URL-safe filters used by one bounded global search. */
export interface SearchQuery {
  query: string
  group: string
  kind: string
  resource: string
  namespace: string
  name: string
  labelSelector: string
}

/** Identifies the aggregate state of one global search response. */
export type SearchState = 'available' | 'partial' | 'empty' | 'permission-denied' | 'unavailable' | 'error'

/** Identifies the state of one exact discovered search source. */
export type SearchSourceState = Exclude<SearchState, 'partial'>

/** Contains complete GVR identity and safe summary fields for one search result. */
export interface SearchResult {
  group: string
  version: string
  resource: string
  kind: string
  scope: ResourceScope | string
  namespaced: boolean
  name: string
  namespace?: string
  createdAt: string
  status: string
  ready?: string
  labels?: Record<string, string>
}

/** Reports one source outcome without returning a Kubernetes object body. */
export interface SearchSource {
  group: string
  version: string
  resource: string
  kind: string
  scope: ResourceScope | string
  namespaced: boolean
  state: SearchSourceState
  resultCount: number
  hasMore?: boolean
  truncated?: boolean
  issue?: string
}

/** Contains bounded global search results, source states, and revision-scoped catalog health. */
export interface SearchResponse {
  context: string
  revision: number
  state: SearchState
  items: SearchResult[]
  sources: SearchSource[]
  issues?: DiscoveryIssue[]
  partial: boolean
  truncated: boolean
  pageSize: number
  callCount: number
  callBudget: number
  pageCount: number
  pageBudget: number
  hasMore: boolean
  nextCursor?: string
}

/** Describes a validated locally persisted search view and its compatibility guard. */
export interface SavedSearchView {
  id: string
  name: string
  context: string
  revision: number
  query: SearchQuery
  catalogFingerprint: string
  createdAt: number
  updatedAt: number
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

/** Summarizes safe top-level sections that differed when a generic update conflicted. */
export interface ResourceConflictSummary {
  localResourceVersion?: string
  currentResourceVersion?: string
  changedSections: string[]
  truncated: boolean
}

/** Contains the safe current snapshot returned for a generic resource-version conflict. */
export interface ResourceConflictResponse {
  error: string
  conflict: boolean
  currentAvailable: boolean
  currentResourceVersion?: string
  currentObject?: ResourceObject
  summary: ResourceConflictSummary
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
  involvedObject?: EventObjectReference
  count: number
  firstSeen: string
  lastSeen: string
}

/** Contains the safe identity fields named by a Kubernetes Event involvedObject. */
export interface EventObjectReference {
  apiVersion: string
  kind: string
  name: string
  namespace?: string
  uid?: string
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

/** Identifies the explicit state of one operational read-pack panel. */
export type OperationalReadPackState = 'available' | 'empty' | 'partial' | 'permission-denied' | 'unavailable' | 'error' | 'stale'

/** Contains safe timing and cap diagnostics for one operational read-pack request. */
export interface OperationalReadPackDiagnostics {
  queryCount: number
  queryBudget: number
  durationMilliseconds: number
  deadlineMilliseconds: number
  resultLimit: number
}

/** Identifies a related Kubernetes object without returning its resource body. */
export interface OperationalReadPackReference {
  apiVersion?: string
  kind?: string
  name: string
  namespace?: string
  state: string
  message?: string
}

/** Contains one safe Kubernetes condition summary. */
export interface OperationalCondition {
  type: string
  status: string
  reason?: string
  message?: string
}

/** Describes one bounded native selector expression. */
export interface OperationalLabelExpression {
  key: string
  operator: string
  values?: string[]
}

/** Contains display-safe LabelSelector values. */
export interface OperationalLabelSelector {
  matchLabels: Record<string, string>
  matchExpressions: OperationalLabelExpression[]
  text: string
}

/** Summarizes one Ingress rule host and HTTP path set. */
export interface IngressRuleSummary {
  host?: string
  paths: IngressPathSummary[]
}

/** Identifies one Ingress path backend. */
export interface IngressPathSummary {
  path?: string
  pathType?: string
  serviceName?: string
  servicePort?: string
  resource?: string
}

/** Identifies one unique Ingress backend. */
export interface IngressBackendSummary {
  name: string
  namespace?: string
  port?: string
  kind?: string
  state: string
}

/** Summarizes one EndpointSlice endpoint set. */
export interface EndpointSliceSummary {
  name: string
  serviceName?: string
  addressType?: string
  ports: string[]
  endpoints: EndpointSummary[]
}

/** Contains safe address and topology state for one EndpointSlice endpoint. */
export interface EndpointSummary {
  addresses: string[]
  ready?: boolean
  serving?: boolean
  terminating?: boolean
  nodeName?: string
  zone?: string
}

/** Contains Ingress rules, backend services, and EndpointSlice visibility. */
export interface IngressReadPackData {
  ingressClass?: string
  tlsHosts: string[]
  rules: IngressRuleSummary[]
  backends: IngressBackendSummary[]
  services: OperationalReadPackReference[]
  endpointSlices: EndpointSliceSummary[]
}

/** Contains the current EndpointSlice summary. */
export interface EndpointSliceReadPackData {
  slice: EndpointSliceSummary
}

/** Summarizes one NetworkPolicy peer and port set. */
export interface NetworkPolicyRuleSummary {
  peerCount: number
  portCount: number
  peers: string[]
  ports: string[]
}

/** Contains NetworkPolicy selector, rules, and bounded affected Pod references. */
export interface NetworkPolicyReadPackData {
  podSelector: OperationalLabelSelector
  policyTypes: string[]
  ingressRules: NetworkPolicyRuleSummary[]
  egressRules: NetworkPolicyRuleSummary[]
  affectedPods: OperationalReadPackReference[]
}

/** Summarizes one HPA metric source and target. */
export interface AutoscalerMetricSummary {
  type: string
  resource?: string
  name?: string
  current?: string
  target?: string
  targetAverage?: string
  targetUtilization?: number
}

/** Contains HPA target, replica bounds, metrics, and conditions. */
export interface HorizontalPodAutoscalerReadPackData {
  target: OperationalReadPackReference
  minReplicas?: number
  maxReplicas?: number
  currentReplicas?: number
  desiredReplicas?: number
  metrics: AutoscalerMetricSummary[]
  conditions: OperationalCondition[]
}

/** Contains PDB availability counters and disruption allowance. */
export interface PodDisruptionBudgetReadPackData {
  minAvailable?: string
  maxUnavailable?: string
  expectedPods?: number
  currentHealthy?: number
  desiredHealthy?: number
  disruptionsAllowed?: number
  conditions: OperationalCondition[]
}

/** Contains ResourceQuota scopes and used/hard quantities. */
export interface ResourceQuotaReadPackData {
  scopes: string[]
  usage: QuotaUsageSummary[]
}

/** Summarizes one quota resource and optional percentage. */
export interface QuotaUsageSummary {
  resource: string
  used?: string
  hard?: string
  percent?: number
}

/** Contains bounded LimitRange defaults and limits. */
export interface LimitRangeReadPackData {
  items: LimitRangeItemSummary[]
}

/** Summarizes one LimitRange item without arbitrary provider fields. */
export interface LimitRangeItemSummary {
  type: string
  default: Record<string, string>
  defaultRequest: Record<string, string>
  max: Record<string, string>
  min: Record<string, string>
}

/** Contains PersistentVolume lifecycle fields and safe claim/class references. */
export interface PersistentVolumeReadPackData {
  phase?: string
  reason?: string
  storageClass?: string
  reclaimPolicy?: string
  volumeMode?: string
  capacity: Record<string, string>
  claimRef: OperationalReadPackReference
  source?: string
  nodeAffinity: boolean
  providerMessage: boolean
  relationships: OperationalReadPackReference[]
}

/** Contains StorageClass provisioning settings and bounded PV relationships. */
export interface StorageClassReadPackData {
  provisioner?: string
  reclaimPolicy?: string
  volumeBindingMode?: string
  allowVolumeExpansion?: boolean
  mountOptions: string[]
  allowedTopologies: number
  persistentVolumes: OperationalReadPackReference[]
}

/** Contains ServiceAccount references and read-only RBAC binding summaries. */
export interface ServiceAccountReadPackData {
  automountServiceAccountToken?: boolean
  secrets: string[]
  imagePullSecrets: string[]
  roleBindings: RBACBindingSummary[]
  clusterRoleBindings: RBACBindingSummary[]
}

/** Summarizes one RoleBinding or ClusterRoleBinding. */
export interface RBACBindingSummary {
  name: string
  namespace?: string
  roleKind?: string
  roleName?: string
  subjects: RBACSubjectSummary[]
}

/** Contains one non-secret RBAC subject identity. */
export interface RBACSubjectSummary {
  kind: string
  name: string
  namespace?: string
}

/** Contains richer Pod scheduling, readiness, probes, volumes, and security summaries. */
export interface PodReadPackData {
  phase?: string
  reason?: string
  nodeName?: string
  schedulerName?: string
  podIP?: string
  conditions: OperationalCondition[]
  readinessGates: string[]
  containers: PodContainerSummary[]
  initContainers: PodContainerSummary[]
  ephemeralContainers: PodContainerSummary[]
  volumes: PodVolumeSummary[]
  securityContext: PodSecurityContextSummary
}

/** Contains one Pod container's status, probes, termination, and security details. */
export interface PodContainerSummary {
  name: string
  image?: string
  ready?: boolean
  started?: boolean
  restartCount?: number
  state?: string
  stateReason?: string
  stateExitCode?: number
  lastTerminationReason?: string
  lastTerminationExitCode?: number
  probes: PodProbeSummary[]
  securityContext: PodContainerSecuritySummary
}

/** Identifies one configured Pod probe handler. */
export interface PodProbeSummary {
  name: string
  configured: boolean
  handler?: string
}

/** Identifies one Pod volume by type and safe object reference. */
export interface PodVolumeSummary {
  name: string
  type: string
  reference?: string
  readOnly?: boolean
}

/** Contains non-secret Pod-level security settings. */
export interface PodSecurityContextSummary {
  runAsUser?: number
  runAsGroup?: number
  runAsNonRoot?: boolean
  fsGroup?: number
  seccompProfileType?: string
  hostNetwork?: boolean
  hostPID?: boolean
  hostIPC?: boolean
}

/** Contains non-secret container-level security settings. */
export interface PodContainerSecuritySummary {
  privileged?: boolean
  allowPrivilegeEscalation?: boolean
  readOnlyRootFilesystem?: boolean
  runAsUser?: number
  runAsNonRoot?: boolean
  seccompProfileType?: string
  capabilities: string[]
}

/** Identifies the bounded state of one Compute quantity value. */
export type ComputeQuantityState = 'available' | 'absent' | 'partial' | 'not-applicable'

/** Identifies a quantity state or an explicit native metrics telemetry state. */
export type ComputeValueState = ComputeQuantityState | MetricsState

/** Contains one bounded CPU or memory quantity and its semantic state. */
export interface ComputeQuantityValue {
  value?: string
  state: ComputeValueState
}

/** Contains CPU and memory values for one object-side Compute quantity family. */
export interface ComputeQuantitySet {
  cpu: ComputeQuantityValue
  memory: ComputeQuantityValue
  state: ComputeQuantityState
  message?: string
}

/** Contains actual CPU and memory usage plus the preserved metrics sample timestamp. */
export interface ComputeUsageSummary {
  cpu: ComputeQuantityValue
  memory: ComputeQuantityValue
  state: MetricsState | 'partial'
  message?: string
  timestamp?: string
}

/** Contains Node or Pod identity, object-side quantities, and native actual usage. */
export interface ComputeReadPackData {
  kind: string
  name: string
  namespace?: string
  requests: ComputeQuantitySet
  limits: ComputeQuantitySet
  capacity: ComputeQuantitySet
  allocatable: ComputeQuantitySet
  usage: ComputeUsageSummary
}

/** Unions every structured data shape returned by the operational read pack. */
export type OperationalReadPackData = IngressReadPackData | EndpointSliceReadPackData | NetworkPolicyReadPackData | HorizontalPodAutoscalerReadPackData | PodDisruptionBudgetReadPackData | ResourceQuotaReadPackData | LimitRangeReadPackData | PersistentVolumeReadPackData | StorageClassReadPackData | ServiceAccountReadPackData | PodReadPackData | ComputeReadPackData

/** Describes one independently readable operational panel. */
export interface OperationalReadPackPanel {
  key: string
  title: string
  state: OperationalReadPackState
  message: string
  truncated?: boolean
  data?: OperationalReadPackData
}

/** Contains the bounded structured views for one built-in Kubernetes resource. */
export interface OperationalReadPackResponse {
  state: OperationalReadPackState
  message: string
  panels: OperationalReadPackPanel[]
  diagnostics: OperationalReadPackDiagnostics
}

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
  deferred?: boolean
}

/** Contains safe relationship query counts and timing data. */
export interface RelatedDiagnostics {
  queryCount: number
  queryBudget: number
  durationMilliseconds: number
  deadlineMilliseconds: number
  concurrentLimit: number
}

/** Contains bounded related-resource targets, source notices, aggregate state, and diagnostics. */
export interface RelatedResourcesResponse {
  state: RelatedAggregateState
  message?: string
  items: RelatedResource[]
  notices?: RelatedRelationshipNotice[]
  truncated?: boolean
  diagnostics?: RelatedDiagnostics
}

/** Represents one browser message exchanged with a native Pod terminal. */
export interface TerminalMessage {
  type: 'input' | 'resize' | 'output' | 'error' | 'closed'
  data?: string
  message?: string
  requestId?: string
  retry?: RetryClassification
  rows?: number
  cols?: number
}

/** Represents the text control messages exchanged around a binary port-forward stream. */
export interface PortForwardMessage {
  type: 'ready' | 'error' | 'closed'
  message?: string
  requestId?: string
  retry?: RetryClassification
  targetPort?: number
  localPort?: number
}

/** Represents the browser route used by the single-page application. */
export interface RouteState {
  path: string
  view?: 'history' | 'catalog' | 'search' | 'diagnostics' | 'workloads'
  search?: string
  group?: string
  version?: string
  resource?: string
  name?: string
  namespace?: string
}
