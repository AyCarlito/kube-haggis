import type { RouteState } from './types'

/** Identifies the lifecycle state of the in-memory write-mode selection. */
export type WriteModeStatus = 'loading' | 'read-only' | 'write'

/** Stores write mode and the active context without any browser persistence. */
export interface WriteModeState {
  status: WriteModeStatus
  context: string
}

/** Describes one lifecycle event that can change the write-mode selection. */
export type WriteModeAction =
  | { type: 'bootstrap-loading' }
  | { type: 'bootstrap-success'; context: string }
  | { type: 'enable'; context: string; route: RouteState }
  | { type: 'reset' }
  | { type: 'navigate'; route: RouteState }
  | { type: 'context-change'; context: string }

/** Creates the read-only state used before the first kubeconfig response arrives. */
export function initialWriteModeState(): WriteModeState {
  return { status: 'loading', context: '' }
}

/** Identifies the cluster dashboard route that always establishes a read-only boundary. */
export function isDashboardRoute(route: RouteState): boolean {
  return route.path === '/' && !route.view && !route.resource
}

/** Reports whether the lifecycle state permits a client mutation request. */
export function isWriteModeEnabled(state: WriteModeState): boolean {
  return state.status === 'write' && Boolean(state.context)
}

/** Applies one write-mode lifecycle event while keeping reset boundaries explicit. */
export function reduceWriteMode(state: WriteModeState, action: WriteModeAction): WriteModeState {
  switch (action.type) {
    case 'bootstrap-loading':
      return { status: 'loading', context: state.context }
    case 'bootstrap-success':
      return { status: 'read-only', context: action.context }
    case 'enable':
      if (state.status === 'loading' || !action.context || action.context !== state.context || isDashboardRoute(action.route)) return state
      return { status: 'write', context: state.context }
    case 'reset':
      return { status: 'read-only', context: state.context }
    case 'navigate':
      return isDashboardRoute(action.route) ? { status: 'read-only', context: state.context } : state
    case 'context-change':
      return { status: 'read-only', context: action.context }
  }
}
