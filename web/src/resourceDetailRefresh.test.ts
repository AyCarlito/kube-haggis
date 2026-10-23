import { ApiError } from './api'
import { resourceRefreshFailed, resourceRefreshIntent, resourceRefreshSucceeded } from './resourceDetailRefresh'
import type { ResourceObject } from './types'

/** Fails a focused resource-detail refresh assertion with a readable message. */
function assertResourceRefresh(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises successful replacement, failure retention, and draft/review confirmation policy. */
export function runResourceDetailRefreshModelTests(): void {
  const previous: ResourceObject = { metadata: { name: 'api', resourceVersion: '7' }, spec: { replicas: 1 } }
  const refreshed: ResourceObject = { metadata: { name: 'api', resourceVersion: '8' }, spec: { replicas: 2 } }
  const success = resourceRefreshSucceeded(refreshed)
  assertResourceRefresh(success.object === refreshed && success.error === '', 'successful refresh did not replace the detail object or clear its error')

  const unavailable = resourceRefreshFailed(previous, new ApiError('not found', 404, { error: 'not found' }))
  assertResourceRefresh(unavailable.object === previous && unavailable.error.includes('Refresh unavailable') && unavailable.error.includes('last known object'), 'failed refresh did not retain the last known object with an unavailable message')

  const forbidden = resourceRefreshFailed(previous, new ApiError('forbidden', 403, { error: 'forbidden' }))
  const conflict = resourceRefreshFailed(previous, new ApiError('conflict', 409, { error: 'conflict' }))
  const network = resourceRefreshFailed(previous, new ApiError('network', 0, undefined))
  assertResourceRefresh(forbidden.error.includes('Refresh forbidden'), 'forbidden refresh was not reported as forbidden')
  assertResourceRefresh(conflict.error.includes('Refresh conflict'), 'conflicting refresh was not reported as a conflict')
  assertResourceRefresh(network.error.includes('could not be reached'), 'network refresh failure was not reported as a network failure')

  assertResourceRefresh(resourceRefreshIntent(false, false) === 'refresh', 'clean detail refresh unexpectedly required confirmation')
  assertResourceRefresh(resourceRefreshIntent(true, false) === 'confirm', 'dirty detail refresh did not require confirmation')
  assertResourceRefresh(resourceRefreshIntent(false, true) === 'confirm', 'pending review refresh did not require confirmation')
}

runResourceDetailRefreshModelTests()
