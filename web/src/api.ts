import type {
  AuditResponse,
  ClusterOverview,
  ContextsResponse,
  DiagnosticsResponse,
  EphemeralContainerRequest,
  PodOperationResponse,
  ResourceDefinition,
  ResourceDeleteItem,
  ResourceDeleteResponse,
  BulkDeleteResponse,
  ResourceConflictResponse,
  ResourceCatalogResponse,
  ResourceEventsResponse,
  ResourceListResponse,
  ResourceObject,
  ResourceSchemaResponse,
  RelatedResourcesResponse,
  OperationalReadPackResponse,
  SearchQuery,
  SearchResponse,
  NodeActionRequest,
  NodeActionResponse,
  PVCActionRequest,
  PVCActionResponse,
  PVCOperationInfo,
  RetryClassification,
  WorkloadActionRequest,
  WorkloadActionResponse,
} from './types'

/** Represents a safe HTTP API failure with its status and structured payload. */
export class ApiError extends Error {
  readonly status: number
  readonly payload: unknown
  readonly requestId?: string
  readonly retry: RetryClassification

  /** Creates an API error that preserves structured response data for safe workflows. */
  constructor(message: string, status: number, payload: unknown, requestId?: string, retry: RetryClassification = 'retry') {
    const details = [requestId ? `request ID ${requestId}` : '', `retry: ${retry}`].filter(Boolean).join('; ')
    super(details ? `${message} (${details})` : message)
    this.name = 'ApiError'
    this.status = status
    this.payload = payload
    this.requestId = requestId
    this.retry = retry
  }
}

/** Returns a bounded safe error string from a backend JSON payload. */
function payloadErrorMessage(payload: unknown, fallback: string): string {
  if (!payload || typeof payload !== 'object' || !('error' in payload) || typeof payload.error !== 'string') return fallback
  const message = payload.error.trim()
  return message ? message.slice(0, 512) : fallback
}

/** Returns the stable retry classification supplied by the backend or inferred from status. */
export function retryClassificationFor(status: number, payload: unknown): RetryClassification {
  if (payload && typeof payload === 'object' && 'retry' in payload && (payload.retry === 'retry' || payload.retry === 'check-and-retry' || payload.retry === 'refresh-and-review')) return payload.retry
  if (status === 409) return 'refresh-and-review'
  if (status === 400 || status === 401 || status === 403 || status === 404 || status === 501) return 'check-and-retry'
  return 'retry'
}

/** Converts one failed HTTP response into a safe API error with its correlation ID. */
export async function responseApiError(response: Response, fallback = `Request failed with ${response.status}`): Promise<ApiError> {
  const payload = (await response.json().catch(() => ({}))) as unknown
  const requestId = response.headers.get('X-Request-ID') || undefined
  return new ApiError(payloadErrorMessage(payload, fallback), response.status, payload, requestId, retryClassificationFor(response.status, payload))
}

/** Wraps a mutation response with the opaque server-bound review handle returned by a dry-run. */
export interface MutationResponse<T> {
  value: T
  reviewToken?: string
}

/** Describes the bounded server-side query controls for one resource page. */
export interface ResourceListQuery {
  limit?: number
  continueToken?: string
}

/** Builds a backend API URL with encoded query parameters. */
export function apiUrl(path: string, params: Record<string, string | undefined> = {}): string {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') search.set(key, value)
  })
  const query = search.toString()
  return query ? `${path}?${query}` : path
}

/** Fetches JSON and turns non-successful responses into readable errors. */
export async function fetchJson<T>(url: string, init?: RequestInit): Promise<T> {
  let response: Response
  try {
    response = await fetch(url, init)
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw error
    throw new ApiError('The local API could not be reached', 0, undefined, undefined, 'retry')
  }
  const payload = (await response.json().catch(() => ({}))) as unknown
  if (!response.ok) throw new ApiError(payloadErrorMessage(payload, `Request failed with ${response.status}`), response.status, payload, response.headers.get('X-Request-ID') || undefined, retryClassificationFor(response.status, payload))
  return payload as T
}

/** Fetches JSON while returning the dedicated review header without exposing it in the response body. */
export async function fetchJsonWithReview<T>(url: string, init: RequestInit | undefined, reviewToken?: string): Promise<MutationResponse<T>> {
	const headers = new Headers(init?.headers)
	if (reviewToken) headers.set('X-KubeHaggis-Review', reviewToken)
	let response: Response
	try {
		response = await fetch(url, { ...init, headers })
	} catch (error) {
		if (error instanceof DOMException && error.name === 'AbortError') throw error
		throw new ApiError('The local API could not be reached', 0, undefined, undefined, 'retry')
	}
	const payload = (await response.json().catch(() => ({}))) as unknown
	if (!response.ok) throw new ApiError(payloadErrorMessage(payload, `Request failed with ${response.status}`), response.status, payload, response.headers.get('X-Request-ID') || undefined, retryClassificationFor(response.status, payload))
	return { value: payload as T, reviewToken: response.headers.get('X-KubeHaggis-Review') || undefined }
}

/** Extracts a dedicated resource-version conflict response without exposing arbitrary backend text. */
export function resourceConflictFromError(error: unknown): ResourceConflictResponse | null {
  if (!(error instanceof ApiError) || error.status !== 409 || !error.payload || typeof error.payload !== 'object') return null
  const payload = error.payload as Partial<ResourceConflictResponse>
  return payload.conflict === true && typeof payload.error === 'string' && payload.summary ? payload as ResourceConflictResponse : null
}

/** Loads the kubeconfig contexts exposed by the backend. */
export function fetchContexts(): Promise<ContextsResponse> {
  return fetchJson<ContextsResponse>('/api/contexts')
}

/** Forces the backend to reload kubeconfig sources without exposing their contents. */
export function reloadContexts(): Promise<ContextsResponse> {
  return fetchJson<ContextsResponse>('/api/contexts/reload', { method: 'POST' })
}

/** Loads the newest bounded slice of redacted local mutation history. */
export function fetchAudit(limit = 500): Promise<AuditResponse> {
  return fetchJson<AuditResponse>(apiUrl('/api/audit', { limit: String(limit) }))
}

/** Loads bounded local diagnostics for the selected context without exposing credentials. */
export function fetchDiagnostics(context: string): Promise<DiagnosticsResponse> {
  return fetchJson<DiagnosticsResponse>(apiUrl('/api/diagnostics', { context }))
}

/** Loads the cluster overview for a kubeconfig context. */
export function fetchOverview(context: string): Promise<ClusterOverview> {
  return fetchJson<ClusterOverview>(apiUrl('/api/overview', { context }))
}

/** Loads the complete discovery-backed resource catalog for a kubeconfig context. */
export function fetchResourceCatalog(context: string, refresh = false): Promise<ResourceCatalogResponse> {
  return fetchJson<ResourceCatalogResponse>(apiUrl('/api/discovery', { context, refresh: refresh ? 'true' : undefined }))
}

/** Runs one explicitly submitted bounded global search against the active catalog. */
export function fetchGlobalSearch(context: string, query: SearchQuery, signal?: AbortSignal): Promise<SearchResponse> {
  return fetchJson<SearchResponse>(
    apiUrl('/api/search', {
      context,
      query: query.query,
      group: query.group,
      kind: query.kind,
      resource: query.resource,
      namespace: query.namespace,
      name: query.name,
      labelSelector: query.labelSelector,
    }),
    { signal },
  )
}

/** Loads discovered API resource definitions for callers that only need catalog items. */
export async function fetchDiscovery(context: string): Promise<ResourceDefinition[]> {
  const response = await fetchResourceCatalog(context)
  return response.items
}

/** Loads a normalized Kubernetes resource schema without exposing kubeconfig credentials to the browser. */
export function fetchResourceSchema(context: string, definition: ResourceDefinition): Promise<ResourceSchemaResponse> {
  return fetchJson<ResourceSchemaResponse>(
    apiUrl('/api/schema', {
      context,
      group: definition.group,
      version: definition.version,
      resource: definition.resource,
    }),
  )
}

/** Loads one generic Kubernetes resource collection. */
export function fetchResources(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  labelSelector = '',
  query: ResourceListQuery = {},
  signal?: AbortSignal,
): Promise<ResourceListResponse> {
  return fetchJson<ResourceListResponse>(
    apiUrl('/api/resources', {
      context,
      group: definition.group,
      version: definition.version,
      resource: definition.resource,
      namespace: definition.namespaced ? namespace : '',
      namespaced: String(definition.namespaced),
      labelSelector,
      limit: String(query.limit ?? 100),
      continue: query.continueToken,
    }),
    { signal },
  )
}

/** Loads one complete unstructured Kubernetes resource. */
export function fetchResource(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  name: string,
): Promise<ResourceObject> {
  return fetchJson<ResourceObject>(
    apiUrl('/api/resource', {
      context,
      group: definition.group,
      version: definition.version,
      resource: definition.resource,
      namespace: definition.namespaced ? namespace : '',
      name,
      namespaced: String(definition.namespaced),
    }),
  )
}

/** Loads bounded, read-only relationships for one complete Kubernetes resource. */
export function fetchRelated(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  name: string,
): Promise<RelatedResourcesResponse> {
  return fetchJson<RelatedResourcesResponse>(
    apiUrl('/api/related', {
      context,
      group: definition.group,
      version: definition.version,
      resource: definition.resource,
      namespace: definition.namespaced ? namespace : '',
      name,
      namespaced: String(definition.namespaced),
    }),
  )
}

/** Loads the deferred targeted native descendant relationship phase for one resource. */
export function fetchRelatedReverse(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  name: string,
): Promise<RelatedResourcesResponse> {
  return fetchJson<RelatedResourcesResponse>(
    apiUrl('/api/related/reverse', {
      context,
      group: definition.group,
      version: definition.version,
      resource: definition.resource,
      namespace: definition.namespaced ? namespace : '',
      name,
      namespaced: String(definition.namespaced),
    }),
  )
}

/** Loads one bounded, read-only built-in operational read pack. */
export function fetchOperationalReadPack(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  name: string,
  signal?: AbortSignal,
): Promise<OperationalReadPackResponse> {
  return fetchJson<OperationalReadPackResponse>(
    apiUrl('/api/read-pack', {
      context,
      group: definition.group,
      version: definition.version,
      resource: definition.resource,
      namespace: definition.namespaced ? namespace : '',
      name,
      namespaced: String(definition.namespaced),
    }),
    { signal },
  )
}

/** Applies a YAML resource document through the generic backend mutation path. */
export function applyResource(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  name: string,
  body: string,
  dryRun: boolean,
  reviewToken?: string,
): Promise<MutationResponse<ResourceObject>> {
  return fetchJsonWithReview<ResourceObject>(
    apiUrl('/api/resource', {
      context,
      group: definition.group,
      version: definition.version,
      resource: definition.resource,
      namespace: definition.namespaced ? namespace : '',
      name: name === 'new' ? undefined : name,
      namespaced: String(definition.namespaced),
      dryRun: String(dryRun),
    }),
    {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/apply-patch+yaml', 'X-KubeHaggis-Write': 'enabled' },
      body,
    },
    reviewToken,
  )
}

/** Runs one generic resource delete as a server-side dry-run or reviewed mutation. */
export function deleteResource(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  name: string,
  expectedResourceVersion: string,
  dryRun: boolean,
  confirmed: boolean,
  confirmName: string,
  reviewToken?: string,
): Promise<MutationResponse<ResourceDeleteResponse>> {
  return fetchJsonWithReview<ResourceDeleteResponse>(
    apiUrl('/api/resource', {
      context,
      group: definition.group,
      version: definition.version,
      resource: definition.resource,
      namespace: definition.namespaced ? namespace : '',
      name,
      namespaced: String(definition.namespaced),
      expectedResourceVersion,
      dryRun: String(dryRun),
      confirmed: String(confirmed),
      confirmName,
    }),
    {
      method: 'DELETE',
      headers: { 'X-KubeHaggis-Write': 'enabled' },
    },
    reviewToken,
  )
}

/** Runs a bounded generic bulk delete with per-item dry-run or mutation results. */
export function deleteResources(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  items: ResourceDeleteItem[],
  dryRun: boolean,
  confirmed: boolean,
  confirmation: string,
  reviewToken?: string,
): Promise<MutationResponse<BulkDeleteResponse>> {
  return fetchJsonWithReview<BulkDeleteResponse>(
    apiUrl('/api/resources/delete', {
      context,
      group: definition.group,
      version: definition.version,
      resource: definition.resource,
      namespace: definition.namespaced ? namespace : '',
      namespaced: String(definition.namespaced),
    }),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-KubeHaggis-Write': 'enabled' },
      body: JSON.stringify({ items, dryRun, confirmed, confirmation }),
    },
    reviewToken,
  )
}

/** Runs one targeted workload operation through the native backend action path. */
export function applyWorkloadAction(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  name: string,
  request: WorkloadActionRequest,
  reviewToken?: string,
): Promise<MutationResponse<WorkloadActionResponse>> {
  return fetchJsonWithReview<WorkloadActionResponse>(
    apiUrl('/api/workload/action', {
      context,
      group: definition.group,
      version: definition.version,
      resource: definition.resource,
      namespace: definition.namespaced ? namespace : '',
      name,
    }),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-KubeHaggis-Write': 'enabled' },
      body: JSON.stringify(request),
    },
    reviewToken,
  )
}

/** Loads native resize and VolumeSnapshot capability states for one PVC. */
export function fetchPVCOperations(context: string, namespace: string, name: string): Promise<PVCOperationInfo> {
  return fetchJson<PVCOperationInfo>(apiUrl('/api/pvc/operations', { context, namespace, name }))
}

/** Runs one reviewed native PVC resize or VolumeSnapshot operation. */
export function applyPVCAction(context: string, namespace: string, name: string, request: PVCActionRequest, reviewToken?: string): Promise<MutationResponse<PVCActionResponse>> {
  return fetchJsonWithReview<PVCActionResponse>(
    apiUrl('/api/pvc/action', { context, namespace, name }),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-KubeHaggis-Write': 'enabled' },
      body: JSON.stringify(request),
    },
    reviewToken,
  )
}

/** Runs one targeted native Node operation through the typed backend action path. */
export function applyNodeAction(context: string, name: string, request: NodeActionRequest, reviewToken?: string): Promise<MutationResponse<NodeActionResponse>> {
  return fetchJsonWithReview<NodeActionResponse>(
    apiUrl('/api/node/action', { context, name }),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-KubeHaggis-Write': 'enabled' },
      body: JSON.stringify(request),
    },
    reviewToken,
  )
}

/** Builds the server-sent events URL for one generic resource watch. */
export function watchUrl(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  resourceVersion: string,
): string {
  return apiUrl('/api/watch', {
    context,
    group: definition.group,
    version: definition.version,
    resource: definition.resource,
    namespace: definition.namespaced ? namespace : '',
    namespaced: String(definition.namespaced),
    resourceVersion,
  })
}

/** Builds the native Pod log stream URL. */
export function logsUrl(context: string, namespace: string, pod: string, container: string, follow: boolean, previous: boolean, tailLines: number): string {
  return apiUrl('/api/logs', {
    context,
    namespace,
    pod,
    container,
    follow: String(follow),
    previous: String(previous),
    tailLines: String(tailLines),
  })
}

/** Launches an ephemeral debug container through the native Pod subresource. */
export function launchEphemeralContainer(context: string, namespace: string, pod: string, request: EphemeralContainerRequest): Promise<PodOperationResponse> {
  return fetchJson<PodOperationResponse>(
    apiUrl('/api/pod/debug', { context, namespace, pod }),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-KubeHaggis-Write': 'enabled' },
      body: JSON.stringify(request),
    },
  )
}

/** Builds the native Pod file-download URL; downloads do not require application write mode. */
export function podCopyDownloadUrl(context: string, namespace: string, pod: string, container: string, remotePath: string): string {
  return apiUrl('/api/pod/copy', { context, namespace, pod, container, path: remotePath })
}

/** Downloads one container file through the local native Kubernetes bridge. */
export async function downloadPodFile(context: string, namespace: string, pod: string, container: string, remotePath: string): Promise<Blob> {
  let response: Response
  try {
    response = await fetch(podCopyDownloadUrl(context, namespace, pod, container, remotePath))
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw error
    throw new ApiError('The local API could not be reached', 0, undefined, undefined, 'retry')
  }
  if (!response.ok) throw await responseApiError(response, `File download failed with ${response.status}`)
  return response.blob()
}

/** Uploads one browser-selected local file through the native Pod exec stream. */
export function uploadPodFile(context: string, namespace: string, pod: string, container: string, remotePath: string, file: File): Promise<PodOperationResponse> {
  const form = new FormData()
  form.set('container', container)
  form.set('path', remotePath)
  form.set('file', file, file.name)
  return fetchJson<PodOperationResponse>(
    apiUrl('/api/pod/copy', { context, namespace, pod }),
    {
      method: 'POST',
      headers: { 'X-KubeHaggis-Write': 'enabled' },
      body: form,
    },
  )
}

/** Builds the browser WebSocket URL for a native Pod terminal session. */
export function terminalUrl(context: string, namespace: string, pod: string, container: string, mode: 'exec' | 'attach', command: string): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params = new URLSearchParams({ context, namespace, pod, mode })
  if (container) params.set('container', container)
  if (mode === 'exec' && command) params.set('command', command)
  return `${protocol}//${window.location.host}/api/terminal?${params.toString()}`
}

/** Builds the browser WebSocket URL for a native Pod port-forward session. */
export function portForwardUrl(context: string, namespace: string, pod: string, targetPort: number, localPort: number): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params = new URLSearchParams({ context, namespace, pod, targetPort: String(targetPort), localPort: String(localPort) })
  return `${protocol}//${window.location.host}/api/port-forward?${params.toString()}`
}

/** Loads Events associated with one resource UID. */
export function fetchResourceEvents(context: string, namespace: string, uid: string, signal?: AbortSignal): Promise<ResourceEventsResponse> {
  return fetchJson<ResourceEventsResponse>(apiUrl('/api/events', { context, namespace, uid }), { signal })
}

/** Builds the server-sent events URL for resource-scoped Events. */
export function resourceEventsWatchUrl(context: string, namespace: string, uid: string, resourceVersion: string): string {
  return apiUrl('/api/events/watch', { context, namespace, uid, resourceVersion })
}
