import { ApiError } from './api'

/** Identifies whether a detail refresh can start immediately or needs confirmation. */
export type ResourceRefreshIntent = 'refresh' | 'confirm'

/** Contains the object and error state produced by one detail refresh attempt. */
export interface ResourceRefreshOutcome<T> {
  object: T | null
  error: string
}

/** Returns whether replacing the current detail editor requires explicit confirmation. */
export function resourceRefreshIntent(draftDirty: boolean, reviewPending: boolean): ResourceRefreshIntent {
  return draftDirty || reviewPending ? 'confirm' : 'refresh'
}

/** Records a successful refresh without retaining a stale refresh error. */
export function resourceRefreshSucceeded<T>(object: T): ResourceRefreshOutcome<T> {
  return { object, error: '' }
}

/** Records a failed refresh while retaining the last known object. */
export function resourceRefreshFailed<T>(object: T | null, error: unknown): ResourceRefreshOutcome<T> {
  return { object, error: resourceRefreshFailureMessage(error) }
}

/** Returns a safe operator-facing message for the supported detail refresh failures. */
export function resourceRefreshFailureMessage(error: unknown): string {
  const detail = error instanceof Error ? error.message.trim() : ''
  const normalizedDetail = detail.toLowerCase()
  const suffix = ' The last known object remains visible.'
  if (error instanceof ApiError) {
    if (error.status === 0) return `Refresh failed: the local API could not be reached.${suffix}`
    if (error.status === 401 || error.status === 403) return `Refresh forbidden: Kubernetes RBAC denied reading this resource.${suffix}`
    if (error.status === 404 || error.status === 410 || error.status === 501 || error.status === 502 || error.status === 503 || error.status === 504) return `Refresh unavailable: the resource or API endpoint could not be read.${suffix}`
    if (error.status === 409) return `Refresh conflict: Kubernetes reported a resource-version conflict while reading this object.${suffix}`
  }
  if (/not found|unavailable|not available/.test(normalizedDetail)) return `Refresh unavailable: the resource or API endpoint could not be read.${suffix}`
  if (error instanceof TypeError || /network|fetch|reach/i.test(detail)) return `Refresh failed: the network read could not be completed.${suffix}`
  return `Refresh failed${detail ? `: ${detail}` : ': the Kubernetes object could not be read'}.${suffix}`
}
