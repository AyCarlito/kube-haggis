import { applyResource, ApiError, setClientWriteModeEnabled } from './api'
import { initialWriteModeState, isDashboardRoute, isWriteModeEnabled, reduceWriteMode, type WriteModeState } from './writeMode'
import type { ResourceDefinition, RouteState } from './types'

/** Fails a focused write-mode assertion with a readable message. */
function assertWriteMode(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Creates a route fixture for one non-dashboard view. */
function resourceRoute(name?: string): RouteState {
  return { path: name ? `/resources/core/v1/pods/${name}` : '/resources/core/v1/pods', group: '', version: 'v1', resource: 'pods', name }
}

/** Creates the smallest resource definition accepted by the generic mutation client. */
function mutationDefinition(): ResourceDefinition {
  return { group: '', version: 'v1', resource: 'configmaps', kind: 'ConfigMap', namespaced: true, scope: 'Namespaced', verbs: ['update'] }
}

/** Verifies that a client mutation cannot emit a write request while the guard is reset. */
function assertWriteAttemptBlocked(message: string): void {
  let caught: unknown
  try {
    void applyResource('field', mutationDefinition(), 'default', 'settings', 'apiVersion: v1\nkind: ConfigMap\n', true)
  } catch (error) {
    caught = error
  }
  assertWriteMode(caught instanceof ApiError && caught.status === 403, message)
}

/** Exercises bootstrap, sticky navigation, reset boundaries, history, and client write guards. */
export function runWriteModeModelTests(): void {
  const loading = initialWriteModeState()
  assertWriteMode(!isWriteModeEnabled(loading), 'write mode was available during kubeconfig bootstrap')
  const bootstrapped = reduceWriteMode(loading, { type: 'bootstrap-success', context: 'field' })
  assertWriteMode(bootstrapped.status === 'read-only' && !isWriteModeEnabled(bootstrapped), 'successful bootstrap did not establish read-only mode')

  const enabled = reduceWriteMode(bootstrapped, { type: 'enable', context: 'field', route: resourceRoute() })
  assertWriteMode(isWriteModeEnabled(enabled), 'explicit enable did not enter write mode')
  const navigated = reduceWriteMode(enabled, { type: 'navigate', route: resourceRoute('api') })
  assertWriteMode(isWriteModeEnabled(navigated), 'write mode did not remain sticky across a resource detail navigation')
  const search = reduceWriteMode(navigated, { type: 'navigate', route: { path: '/search', view: 'search' } })
  assertWriteMode(isWriteModeEnabled(search), 'write mode did not remain sticky across search navigation')

  const dashboard = reduceWriteMode(enabled, { type: 'navigate', route: { path: '/' } })
  assertWriteMode(isDashboardRoute({ path: '/' }) && !isWriteModeEnabled(dashboard), 'dashboard navigation did not reset write mode')
  const historyBackToResource = reduceWriteMode(dashboard, { type: 'navigate', route: resourceRoute('api') })
  assertWriteMode(!isWriteModeEnabled(historyBackToResource), 'browser history returned to a resource with stale write mode')

  const contextReset = reduceWriteMode(enabled, { type: 'context-change', context: 'other' })
  assertWriteMode(contextReset.context === 'other' && !isWriteModeEnabled(contextReset), 'context change did not reset write mode')
  const contextReenabled = reduceWriteMode(contextReset, { type: 'enable', context: 'other', route: resourceRoute() })
  assertWriteMode(isWriteModeEnabled(contextReenabled), 'the new context did not require and accept an explicit enable')

  const bootstrapReset = reduceWriteMode(loading, { type: 'bootstrap-success', context: 'field' })
  setClientWriteModeEnabled(isWriteModeEnabled(bootstrapReset))
  assertWriteAttemptBlocked('a bootstrap reset still allowed a client write attempt')
  const dashboardReset = reduceWriteMode(enabled, { type: 'navigate', route: { path: '/' } })
  setClientWriteModeEnabled(isWriteModeEnabled(dashboardReset))
  assertWriteAttemptBlocked('a dashboard reset still allowed a client write attempt')
  const contextChanged = reduceWriteMode(enabled, { type: 'context-change', context: 'other' })
  setClientWriteModeEnabled(isWriteModeEnabled(contextChanged))
  assertWriteAttemptBlocked('a context reset still allowed a client write attempt')
  setClientWriteModeEnabled(false)

  const typedState: WriteModeState = historyBackToResource
  assertWriteMode(typedState.status === 'read-only', 'history reset state was not retained as read-only')
}

runWriteModeModelTests()
