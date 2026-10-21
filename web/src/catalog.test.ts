import type { ResourceDefinition } from './types'
import {
  catalogEventResourcePath,
  catalogResourcePath,
  catalogResourcePathForSlug,
  groupResourceCatalog,
  isAmbiguousCatalogRoute,
  parseResourceRoute,
  resolveCatalogDefinition,
} from './catalog'

/** Fails a focused catalog model assertion with a readable message. */
function assertCatalog(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises canonical routes, legacy ambiguity protection, and grouped catalog ordering. */
export function runCatalogModelTests(): void {
  const definitions: ResourceDefinition[] = [
    { group: '', version: 'v1', resource: 'pods', kind: 'Pod', namespaced: true, scope: 'Namespaced', verbs: ['get'], subresources: [{ name: 'log', verbs: ['get'] }] },
    { group: 'example.com', version: 'v1', resource: 'widgets', kind: 'Widget', namespaced: true, scope: 'Namespaced', verbs: ['get'] },
    { group: 'other.example.com', version: 'v1', resource: 'widgets', kind: 'OtherWidget', namespaced: false, scope: 'Cluster', verbs: ['get'] },
  ]
  const canonical = catalogResourcePath(definitions[1], 'blue widget')
  const route = parseResourceRoute(canonical, '?namespace=team-a')
  assertCatalog(route.group === 'example.com' && route.version === 'v1' && route.resource === 'widgets' && route.name === 'blue widget' && route.namespace === 'team-a', 'canonical route lost complete GVR identity')
  assertCatalog(resolveCatalogDefinition(route, definitions) === definitions[1], 'canonical route resolved the wrong resource')
  const shortRoute = parseResourceRoute('/resources/widgets', '')
  assertCatalog(isAmbiguousCatalogRoute(shortRoute, definitions), 'ambiguous short route was not identified')
  assertCatalog(resolveCatalogDefinition(shortRoute, definitions) === undefined, 'ambiguous short route silently selected a resource')
  assertCatalog(catalogResourcePathForSlug('widgets', definitions) === '/resources/widgets', 'ambiguous navigation unexpectedly became canonical')
  const eventDefinitions: ResourceDefinition[] = [
    { group: '', version: 'v1', resource: 'events', kind: 'Event', namespaced: true, scope: 'Namespaced', verbs: ['get', 'list'] },
    { group: 'events.k8s.io', version: 'v1', resource: 'events', kind: 'Event', namespaced: true, scope: 'Namespaced', verbs: ['get', 'list'] },
  ]
  assertCatalog(catalogEventResourcePath(eventDefinitions, 'warning') === '/resources/core/v1/events/warning', 'core Event navigation did not retain exact GVR identity')
  assertCatalog(catalogResourcePathForSlug('events', eventDefinitions) === '/resources/events', 'generic ambiguous Event slug silently selected an API')
  const searchRoute = parseResourceRoute('/search', '?namespace=team-a&q=api')
  assertCatalog(searchRoute.view === 'search' && searchRoute.namespace === 'team-a' && searchRoute.search === '?namespace=team-a&q=api', 'global search route did not preserve namespace and query state')
  const groups = groupResourceCatalog(definitions)
  assertCatalog(groups.length === 3 && groups[0].group === '' && groups[1].group === 'example.com', 'catalog groups were not deterministic')
  assertCatalog(groups[0].versions[0].definitions[0].resource === 'pods', 'catalog group rows were not retained')
}

runCatalogModelTests()
