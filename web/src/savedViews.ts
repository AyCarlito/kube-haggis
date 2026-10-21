import { normalizeSearchQuery, searchDefinitionFingerprint, SEARCH_FILTER_MAX } from './search'
import type { ResourceDefinition, SavedSearchView, SearchQuery } from './types'

/** Identifies the single bounded localStorage record used by saved global searches. */
export const SAVED_VIEWS_STORAGE_KEY = 'kube-haggis.saved-search-views.v1'

/** Caps the number of saved searches retained in the browser. */
export const SAVED_VIEWS_MAX = 12

/** Caps the encoded saved-search record before older views are evicted. */
export const SAVED_VIEWS_BYTES_MAX = 48 * 1024

/** Identifies why a saved view cannot currently be applied. */
export type SavedViewState = 'current' | 'stale-context' | 'stale-revision' | 'incompatible'

/** Creates a validated saved view tied to one context, revision, and catalog identity set. */
export function createSavedSearchView(name: string, context: string, revision: number, query: SearchQuery, definitions: ResourceDefinition[], now = Date.now()): SavedSearchView {
  const normalizedName = name.trim().slice(0, 64) || 'Unnamed search'
  const normalizedQuery = normalizeSearchQuery(query)
  return {
    id: `search-${now.toString(36)}-${searchDefinitionFingerprint(definitions, normalizedQuery)}`,
    name: normalizedName,
    context: context.trim(),
    revision,
    query: normalizedQuery,
    catalogFingerprint: searchDefinitionFingerprint(definitions, normalizedQuery),
    createdAt: now,
    updatedAt: now,
  }
}

/** Parses untrusted localStorage JSON and keeps only bounded, structurally valid saved views. */
export function parseSavedSearchViews(raw: string | null): SavedSearchView[] {
  if (!raw) return []
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    const seen = new Set<string>()
    const views = parsed
      .map((value) => validateSavedSearchView(value))
      .filter((value): value is SavedSearchView => value !== null)
      .filter((value) => {
        if (seen.has(value.id)) return false
        seen.add(value.id)
        return true
      })
      .sort((left, right) => right.updatedAt - left.updatedAt)
    return boundSavedSearchViews(views)
  } catch {
    return []
  }
}

/** Loads and validates the bounded saved-view record from an injected browser storage object. */
export function loadSavedSearchViews(storage: Pick<Storage, 'getItem'>): SavedSearchView[] {
  try {
    return parseSavedSearchViews(storage.getItem(SAVED_VIEWS_STORAGE_KEY))
  } catch {
    return []
  }
}

/** Persists validated saved views and returns the bounded list that was attempted. */
export function persistSavedSearchViews(storage: Pick<Storage, 'setItem'>, views: SavedSearchView[]): SavedSearchView[] {
  const bounded = boundSavedSearchViews(views)
  try {
    storage.setItem(SAVED_VIEWS_STORAGE_KEY, JSON.stringify(bounded))
  } catch {
    return []
  }
  return bounded
}

/** Adds or replaces one view while preserving the fixed count and size limits. */
export function upsertSavedSearchView(views: SavedSearchView[], next: SavedSearchView): SavedSearchView[] {
  return boundSavedSearchViews([next, ...views.filter((view) => view.id !== next.id)])
}

/** Removes one saved view by its validated identifier. */
export function removeSavedSearchView(views: SavedSearchView[], id: string): SavedSearchView[] {
  return boundSavedSearchViews(views.filter((view) => view.id !== id))
}

/** Classifies one saved view against the active context, revision, and discovered resource catalog. */
export function savedSearchViewState(view: SavedSearchView, context: string, revision: number, definitions: ResourceDefinition[]): SavedViewState {
  if (view.context !== context) return 'stale-context'
  if (view.revision !== revision) return 'stale-revision'
  if (view.catalogFingerprint !== searchDefinitionFingerprint(definitions, view.query)) return 'incompatible'
  return 'current'
}

/** Returns true only when a saved view is safe to apply without changing cluster or resource identity. */
export function isSavedSearchViewApplicable(view: SavedSearchView, context: string, revision: number, definitions: ResourceDefinition[]): boolean {
  return savedSearchViewState(view, context, revision, definitions) === 'current'
}

/** Validates one parsed JSON value before it can influence a Kubernetes search request. */
export function validateSavedSearchView(value: unknown): SavedSearchView | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const candidate = value as Partial<SavedSearchView>
  if (!boundedText(candidate.id, 96) || !boundedText(candidate.name, 64) || !boundedText(candidate.context, 256) || !boundedText(candidate.catalogFingerprint, 16)) return null
  if (!candidate.id.trim() || !candidate.name.trim() || !candidate.context.trim() || !candidate.catalogFingerprint.trim()) return null
  if (!Number.isInteger(candidate.revision) || (candidate.revision as number) < 0) return null
  if (typeof candidate.createdAt !== 'number' || typeof candidate.updatedAt !== 'number' || !Number.isFinite(candidate.createdAt) || !Number.isFinite(candidate.updatedAt)) return null
  if (!isSearchQuery(candidate.query)) return null
  return {
    id: candidate.id as string,
    name: candidate.name as string,
    context: candidate.context as string,
    revision: candidate.revision as number,
    query: normalizeSearchQuery(candidate.query),
    catalogFingerprint: candidate.catalogFingerprint as string,
    createdAt: candidate.createdAt as number,
    updatedAt: candidate.updatedAt as number,
  }
}

/** Applies count and encoded-size bounds while keeping the newest valid views first. */
function boundSavedSearchViews(views: SavedSearchView[]): SavedSearchView[] {
  const unique = new Map<string, SavedSearchView>()
  views.forEach((view) => {
    if (!unique.has(view.id)) unique.set(view.id, view)
  })
  const next = [...unique.values()].sort((left, right) => right.updatedAt - left.updatedAt).slice(0, SAVED_VIEWS_MAX)
  while (next.length && encodedSize(next) > SAVED_VIEWS_BYTES_MAX) next.pop()
  return next
}

/** Estimates UTF-8 storage size without retaining any unbounded serialized data. */
function encodedSize(views: SavedSearchView[]): number {
  return new TextEncoder().encode(JSON.stringify(views)).length
}

/** Verifies every saved-query field is a bounded string before normalization. */
function isSearchQuery(value: unknown): value is SearchQuery {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const query = value as Partial<SearchQuery>
  return [query.query, query.group, query.kind, query.resource, query.namespace, query.name, query.labelSelector].every((field) => boundedText(field, SEARCH_FILTER_MAX))
}

/** Checks a string field for type, length, and control-character safety. */
function boundedText(value: unknown, maximum: number): value is string {
  return typeof value === 'string' && value.trim().length <= maximum && !/[\u0000\r\n]/.test(value)
}
