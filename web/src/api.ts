import type {
  ClusterOverview,
  ContextsResponse,
  EphemeralContainerRequest,
  PodOperationResponse,
  ResourceDefinition,
  ResourceDeleteItem,
  ResourceDeleteResponse,
  BulkDeleteResponse,
  ResourceEventsResponse,
  ResourceListResponse,
  ResourceObject,
  ResourceSchemaResponse,
  RelatedResourcesResponse,
  NodeActionRequest,
  NodeActionResponse,
  PVCActionRequest,
  PVCActionResponse,
  PVCOperationInfo,
  WorkloadActionRequest,
  WorkloadActionResponse,
} from './types'

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
  const response = await fetch(url, init)
  const payload = (await response.json().catch(() => ({}))) as { error?: string }
  if (!response.ok) throw new Error(payload.error || `Request failed with ${response.status}`)
  return payload as T
}

/** Loads the kubeconfig contexts exposed by the backend. */
export function fetchContexts(): Promise<ContextsResponse> {
  return fetchJson<ContextsResponse>('/api/contexts')
}

/** Loads the cluster overview for a kubeconfig context. */
export function fetchOverview(context: string): Promise<ClusterOverview> {
  return fetchJson<ClusterOverview>(apiUrl('/api/overview', { context }))
}

/** Loads discovered API resource definitions for a kubeconfig context. */
export async function fetchDiscovery(context: string): Promise<ResourceDefinition[]> {
  const response = await fetchJson<{ items: ResourceDefinition[] }>(apiUrl('/api/discovery', { context }))
  return response.items
}

/** Loads a structural CRD schema without exposing kubeconfig credentials to the browser. */
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
      limit: '100',
    }),
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

/** Applies a YAML resource document through the generic backend mutation path. */
export function applyResource(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  body: string,
  dryRun: boolean,
): Promise<ResourceObject> {
  return fetchJson<ResourceObject>(
    apiUrl('/api/resource', {
      context,
      group: definition.group,
      version: definition.version,
      resource: definition.resource,
      namespace: definition.namespaced ? namespace : '',
      namespaced: String(definition.namespaced),
      dryRun: String(dryRun),
    }),
    {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/apply-patch+yaml', 'X-KubeHaggis-Write': 'enabled' },
      body,
    },
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
): Promise<ResourceDeleteResponse> {
  return fetchJson<ResourceDeleteResponse>(
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
): Promise<BulkDeleteResponse> {
  return fetchJson<BulkDeleteResponse>(
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
  )
}

/** Runs one targeted workload operation through the native backend action path. */
export function applyWorkloadAction(
  context: string,
  definition: ResourceDefinition,
  namespace: string,
  name: string,
  request: WorkloadActionRequest,
): Promise<WorkloadActionResponse> {
  return fetchJson<WorkloadActionResponse>(
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
  )
}

/** Loads native resize and VolumeSnapshot capability states for one PVC. */
export function fetchPVCOperations(context: string, namespace: string, name: string): Promise<PVCOperationInfo> {
  return fetchJson<PVCOperationInfo>(apiUrl('/api/pvc/operations', { context, namespace, name }))
}

/** Runs one reviewed native PVC resize or VolumeSnapshot operation. */
export function applyPVCAction(context: string, namespace: string, name: string, request: PVCActionRequest): Promise<PVCActionResponse> {
  return fetchJson<PVCActionResponse>(
    apiUrl('/api/pvc/action', { context, namespace, name }),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-KubeHaggis-Write': 'enabled' },
      body: JSON.stringify(request),
    },
  )
}

/** Runs one targeted native Node operation through the typed backend action path. */
export function applyNodeAction(context: string, name: string, request: NodeActionRequest): Promise<NodeActionResponse> {
  return fetchJson<NodeActionResponse>(
    apiUrl('/api/node/action', { context, name }),
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-KubeHaggis-Write': 'enabled' },
      body: JSON.stringify(request),
    },
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
  const response = await fetch(podCopyDownloadUrl(context, namespace, pod, container, remotePath))
  if (!response.ok) {
    const payload = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(payload.error || `File download failed with ${response.status}`)
  }
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
export function fetchResourceEvents(context: string, namespace: string, uid: string): Promise<ResourceEventsResponse> {
  return fetchJson<ResourceEventsResponse>(apiUrl('/api/events', { context, namespace, uid }))
}

/** Builds the server-sent events URL for resource-scoped Events. */
export function resourceEventsWatchUrl(context: string, namespace: string, uid: string, resourceVersion: string): string {
  return apiUrl('/api/events/watch', { context, namespace, uid, resourceVersion })
}
