import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { contextBootstrapState, NoContextState, type ContextBootstrapSnapshot } from './bootstrap'
import type { ContextsResponse } from './types'

/** Fails a focused bootstrap assertion with a readable message. */
function assertBootstrap(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Creates a loaded context response for deterministic bootstrap state transitions. */
function loadedContexts(currentContext = '', overrides: Partial<ContextsResponse> = {}): ContextsResponse {
  return {
    currentContext,
    contexts: [{ name: 'field', cluster: 'field-cluster', user: 'field-user', server: 'https://field.example.test', isCurrent: currentContext === 'field', reachable: true }],
    sourceState: 'ready',
    revision: 1,
    reloaded: false,
    ...overrides,
  }
}

/** Exercises bootstrap loading, no-context, selected-context, missing, and error states. */
export function runBootstrapModelTests(): void {
  const noContextResponse = loadedContexts()
  const loadingSnapshot: ContextBootstrapSnapshot = { response: noContextResponse, selectedContext: '', loading: true }
  const noContextSnapshot: ContextBootstrapSnapshot = { response: noContextResponse, selectedContext: '', loading: false }
  assertBootstrap(contextBootstrapState({ response: null, selectedContext: '', loading: true }) === 'loading', 'initial bootstrap did not remain loading before the response arrived')
  assertBootstrap(contextBootstrapState(loadingSnapshot) === 'loading', 'bootstrap loading did not win while context data was still loading')
  assertBootstrap(contextBootstrapState(noContextSnapshot) === 'no-context', 'completed bootstrap with no selected context did not become stable no-context')

  const noContextMarkup = renderToStaticMarkup(<NoContextState />)
  assertBootstrap(noContextMarkup.includes('Select a context') && noContextMarkup.includes('switcher above'), 'no-context state did not direct the operator to the context switcher')
  assertBootstrap(!noContextMarkup.includes('Reading local kubeconfig') && !noContextMarkup.includes('spin'), 'no-context state retained bootstrap loading presentation')

  assertBootstrap(contextBootstrapState({ response: loadedContexts('field'), selectedContext: 'field', loading: false }) === 'ready', 'selected context did not preserve the ready bootstrap state')
  assertBootstrap(contextBootstrapState({ response: loadedContexts('', { contexts: [] }), selectedContext: '', loading: false }) === 'setup', 'empty kubeconfig state no longer used the existing setup state')
  assertBootstrap(contextBootstrapState({ response: loadedContexts('field', { error: 'kubeconfig could not be read', sourceState: 'error' }), selectedContext: 'field', loading: false }) === 'setup', 'kubeconfig error state no longer used the existing setup state')
}

runBootstrapModelTests()
