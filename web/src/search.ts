import { catalogResourcePath } from './catalog'
import type { ResourceDefinition, SearchQuery, SearchResult } from './types'

/** Caps each browser-controlled search filter at the backend's safe query size. */
export const SEARCH_FILTER_MAX = 256

/** Defines the user-visible result page sizes supported by global search. */
export const SEARCH_PAGE_SIZES = [25, 50, 100] as const

/** Defines the default number of global search results requested per page. */
export const SEARCH_DEFAULT_PAGE_SIZE = 100

/** Describes one finite API-group choice derived from the loaded discovery catalog. */
export interface SearchGroupOption {
  value: string
  label: string
}

/** Describes one contextual native-datalist choice for a structured search filter. */
export interface SearchSuggestionOption {
  value: string
  label: string
  identities: string[]
}

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

/** Returns deterministic API-group choices while preserving the internal core-group value. */
export function searchGroupOptions(definitions: ResourceDefinition[]): SearchGroupOption[] {
  const groups = new Set(definitions.map((definition) => definition.group || 'core'))
  return [...groups].sort((left, right) => {
    if (left === 'core') return right === 'core' ? 0 : -1
    if (right === 'core') return 1
    return left.localeCompare(right)
  }).map((value) => ({ value, label: value === 'core' ? 'Core API' : value }))
}

/** Returns kind choices limited to the selected API group and annotated with served versions. */
export function searchKindOptions(definitions: ResourceDefinition[], group: string): SearchSuggestionOption[] {
  const options = new Map<string, { value: string; contexts: Set<string>; identities: Set<string> }>()
  definitions.filter((definition) => matchesSearchGroup(definition, group) && definition.kind.trim()).forEach((definition) => {
    const value = definition.kind.trim()
    const valueKey = value.toLowerCase()
    const option = options.get(valueKey) ?? { value, contexts: new Set<string>(), identities: new Set<string>() }
    if (value.localeCompare(option.value) < 0) option.value = value
    option.contexts.add(searchDefinitionContext(definition))
    option.identities.add(`${definition.group}\u001f${definition.version}\u001f${value}`)
    options.set(valueKey, option)
  })
  return [...options.values()].sort(compareSuggestionValues).map((option) => ({
    value: option.value,
    label: `${option.value} (${[...option.contexts].sort(compareText).join(', ')})`,
    identities: [...option.identities].sort(compareText),
  }))
}

/** Returns resource choices limited to the selected API group and Kind context. */
export function searchResourceOptions(definitions: ResourceDefinition[], group: string, kind: string): SearchSuggestionOption[] {
  const normalizedKind = kind.trim().toLowerCase()
  const options = new Map<string, { value: string; contexts: Set<string>; identities: Set<string> }>()
  definitions.filter((definition) => matchesSearchGroup(definition, group) && (!normalizedKind || definition.kind.trim().toLowerCase() === normalizedKind) && definition.resource.trim()).forEach((definition) => {
    const value = definition.resource.trim()
    const valueKey = value.toLowerCase()
    const option = options.get(valueKey) ?? { value, contexts: new Set<string>(), identities: new Set<string>() }
    if (value.localeCompare(option.value) < 0) option.value = value
    option.contexts.add(`${searchDefinitionContext(definition)} / ${definition.kind}`)
    option.identities.add(`${definition.group}\u001f${definition.version}\u001f${value}\u001f${definition.kind.trim()}`)
    options.set(valueKey, option)
  })
  return [...options.values()].sort(compareSuggestionValues).map((option) => ({
    value: option.value,
    label: `${option.value} (${[...option.contexts].sort(compareText).join(', ')})`,
    identities: [...option.identities].sort(compareText),
  }))
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

/** Checks a catalog definition against the search form's explicit group value. */
function matchesSearchGroup(definition: ResourceDefinition, group: string): boolean {
  const normalizedGroup = group.trim().toLowerCase()
  if (!normalizedGroup) return true
  return normalizedGroup === 'core' ? definition.group === '' : definition.group.toLowerCase() === normalizedGroup
}

/** Formats the group and version identity shown beside a native suggestion. */
function searchDefinitionContext(definition: ResourceDefinition): string {
  return `${definition.group || 'Core API'}/${definition.version}`
}

/** Sorts suggestions by their deterministic visible value. */
function compareSuggestionValues(left: { value: string }, right: { value: string }): number {
  return compareText(left.value, right.value)
}

/** Sorts two catalog strings without changing their original casing. */
function compareText(left: string, right: string): number {
  return left.localeCompare(right)
}

/** Trims one browser filter, strips controls, and applies the fixed query-size bound. */
function boundedFilter(value: unknown): string {
  return typeof value === 'string' ? value.replace(/[\u0000\r\n]/g, '').trim().slice(0, SEARCH_FILTER_MAX) : ''
}
