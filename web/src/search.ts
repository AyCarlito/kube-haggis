import { catalogResourcePath } from './catalog'
import type { ResourceDefinition, SearchQuery, SearchResult } from './types'

/** Caps each browser-controlled search filter at the backend's safe query size. */
export const SEARCH_FILTER_MAX = 256

/** Returns the empty search model with an explicit all-namespaces scope. */
export function emptySearchQuery(namespace = 'all'): SearchQuery {
  return { query: '', group: '', kind: '', resource: '', namespace: namespace || 'all', name: '', labelSelector: '' }
}

/** Normalizes a partial search model without allowing control characters or oversized values. */
export function normalizeSearchQuery(value: Partial<SearchQuery>): SearchQuery {
  const next: SearchQuery = {
    query: boundedFilter(value.query),
    group: boundedFilter(value.group),
    kind: boundedFilter(value.kind),
    resource: boundedFilter(value.resource),
    namespace: boundedFilter(value.namespace) || 'all',
    name: boundedFilter(value.name),
    labelSelector: boundedFilter(value.labelSelector),
  }
  return next
}

/** Parses deep-linkable search filters from the browser query string. */
export function parseSearchQuery(search: string): SearchQuery {
  const params = new URLSearchParams(search)
  return normalizeSearchQuery({
    query: params.get('q') || params.get('query') || '',
    group: params.get('group') || '',
    kind: params.get('kind') || '',
    resource: params.get('resource') || '',
    namespace: params.get('namespace') || 'all',
    name: params.get('name') || '',
    labelSelector: params.get('labelSelector') || '',
  })
}

/** Converts search filters into stable URL parameters while omitting empty controls. */
export function searchQueryParams(query: SearchQuery): URLSearchParams {
  const normalized = normalizeSearchQuery(query)
  const params = new URLSearchParams()
  params.set('namespace', normalized.namespace)
  const fields: Array<[string, string]> = [
    ['q', normalized.query],
    ['group', normalized.group],
    ['kind', normalized.kind],
    ['resource', normalized.resource],
    ['name', normalized.name],
    ['labelSelector', normalized.labelSelector],
  ]
  fields.forEach(([key, value]) => {
    if (value) params.set(key, value)
  })
  return params
}

/** Builds a deep-linkable search URL with cluster context and every active filter. */
export function searchHref(context: string, query: SearchQuery): string {
  const params = searchQueryParams(query)
  if (context) params.set('context', context)
  return `/search?${params.toString()}`
}

/** Builds the canonical full-GVR resource route for a safe search result. */
export function searchResultResourcePath(result: SearchResult): string {
  return catalogResourcePath({
    group: result.group,
    version: result.version,
    resource: result.resource,
    kind: result.kind,
    namespaced: result.namespaced,
    scope: result.scope === 'Cluster' ? 'Cluster' : 'Namespaced',
    verbs: [],
  }, result.name)
}

/** Returns a stable coordinate key for result reconciliation and React row identity. */
export function searchResultKey(result: SearchResult): string {
  return [result.group, result.version, result.resource, result.namespace || '', result.name].join('\u001f')
}

/** Identifies whether a search has an explicit filter worth running on submit or deep-link load. */
export function hasSearchFilters(query: SearchQuery): boolean {
  const normalized = normalizeSearchQuery(query)
  return Boolean(normalized.query || normalized.group || normalized.kind || normalized.resource || normalized.name || normalized.labelSelector)
}

/** Compares all URL-backed search filters without depending on object identity. */
export function searchQueryEquals(left: SearchQuery, right: SearchQuery): boolean {
  return left.query === right.query && left.group === right.group && left.kind === right.kind && left.resource === right.resource && left.namespace === right.namespace && left.name === right.name && left.labelSelector === right.labelSelector
}

/** Filters catalog rows using the same exact identity and free-text candidate rules as the backend. */
export function searchDefinitionCandidates(definitions: ResourceDefinition[], query: SearchQuery): ResourceDefinition[] {
	const normalized = normalizeSearchQuery(query)
	const candidates = definitions.filter((definition) => searchDefinitionMatchesFilters(definition, normalized))
	return candidates
}

/** Produces a short catalog fingerprint used to reject saved views after resource identity changes. */
export function searchDefinitionFingerprint(definitions: ResourceDefinition[], query: SearchQuery): string {
  const identity = searchDefinitionCandidates(definitions, query)
    .map((definition) => `${definition.group}\u001f${definition.version}\u001f${definition.resource}`)
    .sort()
    .join('\u001e')
  let hash = 2166136261
  for (let index = 0; index < identity.length; index += 1) hash = Math.imul(hash ^ identity.charCodeAt(index), 16777619)
  return (hash >>> 0).toString(16).padStart(8, '0')
}

/** Compares exact API group, kind, and resource filters without resolving ambiguous short routes. */
function searchDefinitionMatchesFilters(definition: ResourceDefinition, query: SearchQuery): boolean {
  if (query.group) {
    if (query.group.toLowerCase() === 'core' ? definition.group !== '' : definition.group.toLowerCase() !== query.group.toLowerCase()) return false
  }
  if (query.kind && definition.kind.toLowerCase() !== query.kind.toLowerCase()) return false
  if (query.resource && definition.resource.toLowerCase() !== query.resource.toLowerCase()) return false
  return true
}

/** Trims one browser filter, strips controls, and applies the fixed query-size bound. */
function boundedFilter(value: unknown): string {
  return typeof value === 'string' ? value.replace(/[\u0000\r\n]/g, '').trim().slice(0, SEARCH_FILTER_MAX) : ''
}
