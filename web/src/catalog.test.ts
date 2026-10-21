import type { ResourceDefinition } from './types'
import {
  catalogEventResourcePath,
  catalogResourcePath,
  catalogResourcePathForSlug,
  catalogStaticNavigationPath,
  filterResourceCatalog,
  groupResourceCatalog,
  isAmbiguousCatalogRoute,
  parseResourceRoute,
  resourceCatalogMatchesFilter,
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
    { group: 'example.com', version: 'v1', resource: 'widgets', kind: 'Widget', namespaced: true, scope: 'Namespaced', verbs: ['get'], shortNames: ['wdg'], categories: ['all'] },
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
  const duplicateMetricsDefinitions: ResourceDefinition[] = [
    definitions[0],
    { group: 'metrics.k8s.io', version: 'v1beta1', resource: 'pods', kind: 'PodMetrics', namespaced: true, scope: 'Namespaced', verbs: ['get'] },
    { group: '', version: 'v1', resource: 'nodes', kind: 'Node', namespaced: false, scope: 'Cluster', verbs: ['get'] },
    { group: 'metrics.k8s.io', version: 'v1beta1', resource: 'nodes', kind: 'NodeMetrics', namespaced: false, scope: 'Cluster', verbs: ['get'] },
  ]
  assertCatalog(catalogStaticNavigationPath('pods', duplicateMetricsDefinitions) === '/resources/core/v1/pods', 'static Pod navigation did not select the canonical core GVR')
  assertCatalog(catalogStaticNavigationPath('nodes', duplicateMetricsDefinitions) === '/resources/core/v1/nodes', 'static Node navigation did not select the canonical core GVR')
  assertCatalog(catalogStaticNavigationPath('widgets', definitions) === '/resources/widgets', 'unknown ambiguous static navigation did not retain its guarded short route')
  const ingressDefinition: ResourceDefinition = { group: 'networking.k8s.io', version: 'v1', resource: 'ingresses', kind: 'Ingress', namespaced: true, scope: 'Namespaced', verbs: ['get', 'list'] }
  const ingressDefinitions: ResourceDefinition[] = [
    ingressDefinition,
    { group: 'metrics.k8s.io', version: 'v1beta1', resource: 'ingresses', kind: 'IngressMetrics', namespaced: true, scope: 'Namespaced', verbs: ['get', 'list'] },
  ]
  const ingressPath = catalogStaticNavigationPath('ingresses', ingressDefinitions, 'edge')
  assertCatalog(ingressPath === '/resources/group-networking.k8s.io/v1/ingresses/edge', 'static Ingress navigation did not select networking.k8s.io/v1 over an alternate API')
  const ingressRoute = parseResourceRoute(ingressPath, '?context=field&namespace=team-a')
  assertCatalog(ingressRoute.group === 'networking.k8s.io' && ingressRoute.version === 'v1' && ingressRoute.resource === 'ingresses' && ingressRoute.name === 'edge' && ingressRoute.namespace === 'team-a', 'Ingress navigation lost its exact GVR or namespace state')
  assertCatalog(resolveCatalogDefinition(ingressRoute, ingressDefinitions) === ingressDefinition, 'canonical Ingress navigation resolved the alternate API resource')
  const alternateOnlyIngressDefinitions = ingressDefinitions.filter((definition) => definition !== ingressDefinition)
  assertCatalog(catalogStaticNavigationPath('ingresses', alternateOnlyIngressDefinitions) === '/resources/group-networking.k8s.io/v1/ingresses', 'unserved Ingress navigation guessed an alternate API resource')
  assertCatalog(resolveCatalogDefinition(parseResourceRoute('/resources/group-networking.k8s.io/v1/ingresses', ''), alternateOnlyIngressDefinitions) === undefined, 'unserved Ingress navigation did not preserve the explicit unavailable state')
  const eventDefinitions: ResourceDefinition[] = [
    { group: '', version: 'v1', resource: 'events', kind: 'Event', namespaced: true, scope: 'Namespaced', verbs: ['get', 'list'] },
    { group: 'events.k8s.io', version: 'v1', resource: 'events', kind: 'Event', namespaced: true, scope: 'Namespaced', verbs: ['get', 'list'] },
  ]
  assertCatalog(catalogEventResourcePath(eventDefinitions, 'warning') === '/resources/core/v1/events/warning', 'core Event navigation did not retain exact GVR identity')
  assertCatalog(catalogResourcePathForSlug('events', eventDefinitions) === '/resources/events', 'generic ambiguous Event slug silently selected an API')
  const searchRoute = parseResourceRoute('/search', '?namespace=team-a&q=api')
  assertCatalog(searchRoute.view === 'search' && searchRoute.namespace === 'team-a' && searchRoute.search === '?namespace=team-a&q=api', 'global search route did not preserve namespace and query state')
  const workloadRoute = parseResourceRoute('/workloads', '?namespace=team-a')
  assertCatalog(workloadRoute.view === 'workloads' && workloadRoute.namespace === 'team-a', 'workload route did not preserve its view and namespace state')
  const groups = groupResourceCatalog(definitions)
  assertCatalog(groups.length === 3 && groups[0].group === '' && groups[1].group === 'example.com', 'catalog groups were not deterministic')
  assertCatalog(groups[0].versions[0].definitions[0].resource === 'pods', 'catalog group rows were not retained')
  assertCatalog(resourceCatalogMatchesFilter(definitions[0], 'core pod'), 'core group and kind were not searchable together')
  assertCatalog(resourceCatalogMatchesFilter(definitions[1], 'wdg all'), 'short names and categories were not searchable')
  assertCatalog(!resourceCatalogMatchesFilter(definitions[1], 'daemonset'), 'unrelated catalog definition unexpectedly matched')
  assertCatalog(filterResourceCatalog(definitions, 'v1 pods').length === 1, 'catalog version and resource name were not searchable')
  const filtered = filterResourceCatalog(definitions, 'example widget wdg')
  assertCatalog(filtered.length === 1 && filtered[0] === definitions[1], 'catalog filtering did not retain only matching definitions')
  assertCatalog(groupResourceCatalog(filtered).length === 1 && groupResourceCatalog(filtered)[0].group === 'example.com', 'filtered catalog groups were not reduced to matching groups')
}

runCatalogModelTests()
