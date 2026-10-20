import type {
  ClusterOverview,
  ContextsResponse,
  ResourceDefinition,
  ResourceEventsResponse,
  ResourceListResponse,
  ResourceObject,
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
