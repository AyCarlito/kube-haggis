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
