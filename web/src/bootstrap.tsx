import React from 'react'
import { Server } from 'lucide-react'
import type { ContextsResponse } from './types'

/** Identifies the top-level state of kubeconfig bootstrap and context selection. */
export type ContextBootstrapState = 'loading' | 'no-context' | 'setup' | 'ready'

/** Describes the browser state needed to resolve the bootstrap view. */
export interface ContextBootstrapSnapshot {
  response: ContextsResponse | null
  selectedContext: string
  loading: boolean
}

/** Resolves whether the shell should show loading, setup, no-context, or cluster content. */
export function contextBootstrapState(snapshot: ContextBootstrapSnapshot): ContextBootstrapState {
  if (snapshot.loading || !snapshot.response) return 'loading'
  if (snapshot.response.error || snapshot.response.contexts.length === 0) return 'setup'
  if (!snapshot.selectedContext) return 'no-context'
  return 'ready'
}

/** Renders the stable instruction shown after kubeconfig loading completes without a selection. */
export function NoContextState() {
  return (
    <div className="state-page">
      <div className="state-card">
        <span className="state-icon"><Server size={22} /></span>
        <span className="eyebrow"><span className="eyebrow-mark" /> Context required</span>
        <h1>Select a context</h1>
        <p>Choose a context from the switcher above to begin reading cluster data. No discovery or Kubernetes resource reads start until a context is selected.</p>
      </div>
    </div>
  )
}
