import type { ResourceDefinition, RouteState } from './types'

/** Describes one served version within a grouped resource catalog. */
export interface CatalogVersionGroup {
  version: string
  definitions: ResourceDefinition[]
}

/** Describes one API group and its deterministic version sections. */
export interface CatalogGroup {
  group: string
  versions: CatalogVersionGroup[]
}

/** Parses a browser pathname and query string into a catalog-aware route. */
export function parseResourceRoute(pathname: string, search: string): RouteState {
  const parts = pathname.split('/').filter(Boolean)
  if (parts[0] === 'history') return { path: '/history', view: 'history' }
  if (parts[0] === 'diagnostics') return { path: '/diagnostics', view: 'diagnostics' }
  if (parts[0] === 'search') return { path: '/search', view: 'search', search, namespace: new URLSearchParams(search).get('namespace') || undefined }
  if (parts[0] === 'catalog') return { path: '/catalog', view: 'catalog' }
  if (parts[0] === 'workloads') return { path: '/workloads', view: 'workloads', namespace: new URLSearchParams(search).get('namespace') || undefined }
  if (parts[0] !== 'resources' || !parts[1]) return { path: '/' }
  const namespace = new URLSearchParams(search).get('namespace') || undefined
  if (parts.length >= 4) {
    const routeGroup = decodeURIComponent(parts[1])
    const group = routeGroup === 'core' ? '' : routeGroup.startsWith('group-') ? decodeURIComponent(routeGroup.slice('group-'.length)) : routeGroup
    return { path: pathname, group, version: decodeURIComponent(parts[2]), resource: decodeURIComponent(parts[3]), name: parts[4] ? decodeURIComponent(parts[4]) : undefined, namespace }
  }
  return { path: pathname, resource: decodeURIComponent(parts[1]), name: parts[2] ? decodeURIComponent(parts[2]) : undefined, namespace }
}

/** Resolves an exact GVR route or one unambiguous legacy resource slug. */
export function resolveCatalogDefinition(route: RouteState, definitions: ResourceDefinition[]): ResourceDefinition | undefined {
  if (!route.resource) return undefined
  if (route.group !== undefined && route.version) return definitions.find((item) => item.group === route.group && item.version === route.version && item.resource === route.resource)
  const matches = definitions.filter((item) => item.resource === route.resource)
  return matches.length === 1 ? matches[0] : undefined
}

/** Identifies a legacy short resource route that matches multiple discovered GVRs. */
export function isAmbiguousCatalogRoute(route: RouteState, definitions: ResourceDefinition[]): boolean {
  return Boolean(route.resource && route.group === undefined && definitions.filter((item) => item.resource === route.resource).length > 1)
}

/** Builds the canonical group/version/resource route used by new deep links. */
export function catalogResourcePath(definition: ResourceDefinition, name?: string): string {
  const group = definition.group ? `group-${encodeURIComponent(definition.group)}` : 'core'
  const base = `/resources/${group}/${encodeURIComponent(definition.version)}/${encodeURIComponent(definition.resource)}`
  return name === undefined ? base : `${base}/${encodeURIComponent(name)}`
}

/** Keeps a short route only when the loaded catalog has exactly one matching resource name. */
export function catalogResourcePathForSlug(resource: string, definitions: ResourceDefinition[], name?: string): string {
	const matches = definitions.filter((item) => item.resource === resource)
	if (matches.length === 1) return catalogResourcePath(matches[0], name)
	const base = `/resources/${encodeURIComponent(resource)}`
	return name === undefined ? base : `${base}/${encodeURIComponent(name)}`
}

/** Builds an exact Event route for core/v1 when both Kubernetes Event APIs are discovered. */
export function catalogEventResourcePath(definitions: ResourceDefinition[], name?: string): string {
	const coreEvents = definitions.find((item) => item.group === '' && item.version === 'v1' && item.resource === 'events')
	return coreEvents ? catalogResourcePath(coreEvents, name) : catalogResourcePathForSlug('events', definitions, name)
}

/** Determines whether a catalog definition matches every term in a local search query. */
export function resourceCatalogMatchesFilter(definition: ResourceDefinition, query: string): boolean {
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (!terms.length) return true
  const group = definition.group || 'core'
  const searchableText = [
    group,
    `${group}/${definition.version}`,
    definition.version,
    definition.resource,
    definition.kind,
    ...(definition.shortNames ?? []),
    ...(definition.categories ?? []),
  ].join(' ').toLowerCase()
  return terms.every((term) => searchableText.includes(term))
}

/** Filters already loaded catalog definitions without performing a discovery request. */
export function filterResourceCatalog(definitions: ResourceDefinition[], query: string): ResourceDefinition[] {
  return definitions.filter((definition) => resourceCatalogMatchesFilter(definition, query))
}

/** Groups catalog rows by API group and version in deterministic display order. */
export function groupResourceCatalog(definitions: ResourceDefinition[]): CatalogGroup[] {
  const groups = new Map<string, Map<string, ResourceDefinition[]>>()
  definitions.forEach((definition) => {
    const group = definition.group || ''
    const versions = groups.get(group) ?? new Map<string, ResourceDefinition[]>()
    const resources = versions.get(definition.version) ?? []
    resources.push(definition)
    versions.set(definition.version, resources)
    groups.set(group, versions)
  })
  return [...groups.entries()].sort((left, right) => (left[0] || '').localeCompare(right[0] || '')).map(([group, versions]) => ({
    group,
    versions: [...versions.entries()].sort((left, right) => left[0].localeCompare(right[0])).map(([version, resources]) => ({
      version,
      definitions: resources.sort((left, right) => `${left.kind}/${left.resource}`.localeCompare(`${right.kind}/${right.resource}`)),
    })),
  }))
}
