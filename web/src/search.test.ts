import {
  hasSearchFilters,
  normalizeSearchQuery,
  parseSearchQuery,
  searchGroupOptions,
  searchDefinitionFingerprint,
  searchHref,
  searchKindOptions,
  searchQueryEquals,
  searchResourceOptions,
  searchResultResourcePath,
} from './search'
import {
  createSavedSearchView,
  isSavedSearchViewApplicable,
  parseSavedSearchViews,
  persistSavedSearchViews,
  savedSearchViewState,
  SAVED_VIEWS_BYTES_MAX,
  SAVED_VIEWS_MAX,
} from './savedViews'
import type { ResourceDefinition, SavedSearchView } from './types'

/** Fails a focused global-search model assertion with a readable message. */
function assertSearchModel(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises URL parsing, canonical GVR navigation, saved-view guards, and bounded persistence. */
export function runSearchModelTests(): void {
  const definitions: ResourceDefinition[] = [
    { group: '', version: 'v1', resource: 'pods', kind: 'Pod', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
    { group: 'apps', version: 'v1', resource: 'deployments', kind: 'Deployment', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
  ]
  const query = parseSearchQuery('?context=test&namespace=team-a&q=api&group=apps&kind=Deployment&resource=deployments&name=api&labelSelector=app%3Dapi')
  assertSearchModel(query.namespace === 'team-a' && query.query === 'api' && query.group === 'apps' && query.kind === 'Deployment' && query.resource === 'deployments' && query.name === 'api' && query.labelSelector === 'app=api', 'search query parsing lost a deep-link filter')
  assertSearchModel(hasSearchFilters(query), 'explicit search filters were not detected')
  assertSearchModel(searchQueryEquals(query, parseSearchQuery(searchHref('test', query).slice(searchHref('test', query).indexOf('?')))), 'search URL round-trip changed filter state')
  assertSearchModel(searchHref('test', query).includes('namespace=team-a') && searchHref('test', query).includes('labelSelector=app%3Dapi'), 'search href did not preserve scope and selector')
  assertSearchModel(searchResultResourcePath({ group: 'apps', version: 'v1', resource: 'deployments', kind: 'Deployment', scope: 'Namespaced', namespaced: true, name: 'blue api', namespace: 'team-a', createdAt: '', status: 'Active' }) === '/resources/group-apps/v1/deployments/blue%20api', 'search result route did not retain complete GVR identity')

  const current = createSavedSearchView('API deployments', 'test', 4, query, definitions, 100)
  const encoded = JSON.stringify([current, { id: 'bad', name: 'bad' }])
  assertSearchModel(parseSavedSearchViews(encoded).length === 1, 'saved-view parsing accepted an incompatible object')
  assertSearchModel(savedSearchViewState(current, 'test', 4, definitions) === 'current' && isSavedSearchViewApplicable(current, 'test', 4, definitions), 'current saved view was not applicable')
  assertSearchModel(savedSearchViewState(current, 'other', 4, definitions) === 'stale-context', 'saved view context mismatch was not blocked')
  assertSearchModel(savedSearchViewState(current, 'test', 5, definitions) === 'stale-revision', 'saved view revision mismatch was not blocked')
  const changedDefinitions = definitions.concat({ group: 'apps', version: 'v1beta1', resource: 'deployments', kind: 'Deployment', namespaced: true, scope: 'Namespaced', verbs: ['list'] })
  assertSearchModel(searchDefinitionFingerprint(changedDefinitions, query) !== current.catalogFingerprint, 'catalog changes did not invalidate saved resource identity')
  assertSearchModel(savedSearchViewState(current, 'test', 4, changedDefinitions) === 'incompatible', 'incompatible saved resource view was not blocked')

  const autocompleteDefinitions: ResourceDefinition[] = [
    { group: '', version: 'v1', resource: 'pods', kind: 'Pod', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
    { group: 'apps', version: 'v1', resource: 'widgets', kind: 'Widget', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
    { group: 'apps', version: 'v1', resource: 'widgets', kind: 'Widget', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
    { group: 'apps', version: 'v1beta1', resource: 'widgets', kind: 'Widget', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
    { group: 'example.com', version: 'v1', resource: 'widgets', kind: 'Widget', namespaced: false, scope: 'Cluster', verbs: ['list'] },
    { group: 'apps', version: 'v1', resource: 'deployments', kind: 'Deployment', namespaced: true, scope: 'Namespaced', verbs: ['list'] },
  ]
  const autocompleteGroups = searchGroupOptions(autocompleteDefinitions)
  assertSearchModel(autocompleteGroups[0].value === 'core' && autocompleteGroups[0].label === 'Core API', 'core API group did not retain its internal value and clear label')
  assertSearchModel(autocompleteGroups.some((option) => option.value === 'apps' && option.label === 'apps') && autocompleteGroups.length === 3, 'API group suggestions were not derived deterministically from the catalog')

  const allKindOptions = searchKindOptions(autocompleteDefinitions, '')
  const widgetKindOption = allKindOptions.find((option) => option.value === 'Widget')
  assertSearchModel(Boolean(widgetKindOption) && widgetKindOption?.identities.length === 3, 'duplicate kind identities were not aggregated without losing version context')
  assertSearchModel(Boolean(widgetKindOption) && (widgetKindOption?.label.includes('apps/v1') ?? false) && (widgetKindOption?.label.includes('example.com/v1') ?? false), 'ambiguous kind suggestions did not include group/version context')
  const coreKindOptions = searchKindOptions(autocompleteDefinitions, 'core')
  assertSearchModel(coreKindOptions.length === 1 && coreKindOptions[0].value === 'Pod', 'core kind suggestions did not constrain the catalog to the core API')
  const appsKindOptions = searchKindOptions(autocompleteDefinitions, 'apps')
  assertSearchModel(appsKindOptions.length === 2 && appsKindOptions[0].value === 'Deployment' && appsKindOptions[1].value === 'Widget', 'kind suggestions did not constrain to the selected API group')

  const widgetResourceOptions = searchResourceOptions(autocompleteDefinitions, '', 'widget')
  const widgetResourceOption = widgetResourceOptions.find((option) => option.value === 'widgets')
  assertSearchModel(Boolean(widgetResourceOption) && widgetResourceOption?.identities.length === 3, 'duplicate resource identities were not deduplicated while preserving served versions')
  assertSearchModel(Boolean(widgetResourceOption) && (widgetResourceOption?.label.includes('apps/v1 / Widget') ?? false) && (widgetResourceOption?.label.includes('example.com/v1 / Widget') ?? false), 'ambiguous resource suggestions did not include group/version/kind context')
  assertSearchModel(searchResourceOptions(autocompleteDefinitions, 'apps', 'Deployment').length === 1 && searchResourceOptions(autocompleteDefinitions, 'apps', 'Deployment')[0].value === 'deployments', 'resource suggestions did not constrain to the selected group and Kind')
  assertSearchModel(searchResourceOptions(autocompleteDefinitions, 'core', '').length === 1 && searchResourceOptions(autocompleteDefinitions, 'core', '')[0].value === 'pods', 'core resource suggestions did not use the empty Kubernetes API group')

  const freeForm = normalizeSearchQuery({ group: 'apps', kind: 'CustomKind', resource: 'customresources', namespace: 'all' })
  assertSearchModel(freeForm.kind === 'CustomKind' && freeForm.resource === 'customresources', 'free-form Kind or Resource text was rewritten during normalization')
  assertSearchModel(searchResourceOptions(autocompleteDefinitions, freeForm.group, freeForm.kind).length === 0, 'unknown free-form Kind unexpectedly selected a catalog resource')

  const views: SavedSearchView[] = []
  for (let index = 0; index < SAVED_VIEWS_MAX + 4; index += 1) views.push(createSavedSearchView(`View ${index}`, 'test', 4, query, definitions, 100 + index))
  let stored = ''
  const storage = { setItem: (_key: string, value: string) => { stored = value } }
  const bounded = persistSavedSearchViews(storage, views)
  assertSearchModel(bounded.length === SAVED_VIEWS_MAX, 'saved views exceeded the fixed count bound')
  assertSearchModel(new TextEncoder().encode(stored).length <= SAVED_VIEWS_BYTES_MAX, 'saved views exceeded the fixed storage bound')
  assertSearchModel(parseSavedSearchViews(stored).length === SAVED_VIEWS_MAX, 'bounded saved views could not be loaded again')
}

runSearchModelTests()
