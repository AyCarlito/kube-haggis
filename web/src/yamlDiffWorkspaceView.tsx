import { AlertTriangle, Check, FileCode2, LoaderCircle } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { yamlDiffWorkspaceAccessibility, yamlDiffWorkspaceComparisonAvailable, yamlDiffWorkspaceModeForWidth, isYamlDiffWorkspaceEscapeKey, YAML_DIFF_WORKSPACE_PANE_ID, type YamlDiffWorkspaceMode } from './yamlDiffWorkspace'
import { YamlMonacoEditor } from './yamlMonaco'
import type { YamlDiffModel } from './yamlDiff'

/** Defines the baseline, draft, and responsive controls for one YAML workspace. */
export interface YamlDiffWorkspaceProps {
  value: string
  baseline: string
  readOnly: boolean
  scopeKey: string
  onChange: (value: string) => void
  model: YamlDiffModel
  diffOpen: boolean
  onDiffOpenChange: (open: boolean) => void
}

/** Returns the stable operator-facing label for one browser-local diff state. */
function yamlDiffStatusLabel(status: YamlDiffModel['status']): string {
  if (status === 'clean') return 'Clean'
  if (status === 'changed') return 'Draft differs'
  if (status === 'invalid') return 'Incomplete draft'
  if (status === 'bounded') return 'Bounded'
  if (status === 'computing') return 'Calculating'
  return 'No snapshot'
}

/** Returns the status icon without exposing any source document content. */
function yamlDiffStatusIcon(status: YamlDiffModel['status']): ReactNode {
  return status === 'clean' ? <Check size={14} /> : status === 'computing' ? <LoaderCircle size={14} className="spin" /> : <AlertTriangle size={14} />
}

/** Renders the native Monaco workspace with a single modified draft model. */
export function YamlDiffWorkspace({ value, baseline, readOnly, scopeKey, onChange, model, diffOpen, onDiffOpenChange }: YamlDiffWorkspaceProps) {
  const toggleRef = useRef<HTMLButtonElement | null>(null)
  const wasOpenRef = useRef(diffOpen)
  const [workspaceMode, setWorkspaceMode] = useState<YamlDiffWorkspaceMode>(() => yamlDiffWorkspaceModeForWidth(typeof window === 'undefined' ? YAML_DIFF_WORKSPACE_DEFAULT_WIDTH : window.innerWidth))
  const accessibility = yamlDiffWorkspaceAccessibility(diffOpen)
  const comparisonAvailable = yamlDiffWorkspaceComparisonAvailable(baseline, model.status)
  const statusLabel = yamlDiffStatusLabel(model.status)
  const statusIcon = yamlDiffStatusIcon(model.status)

  useEffect(() => {
    /** Keeps the responsive class aligned with the CSS breakpoint after viewport resizing. */
    function handleViewportResize(): void {
      setWorkspaceMode(yamlDiffWorkspaceModeForWidth(window.innerWidth))
    }
    window.addEventListener('resize', handleViewportResize)
    return () => window.removeEventListener('resize', handleViewportResize)
  }, [])

  useEffect(() => {
    if (!diffOpen && wasOpenRef.current) {
      wasOpenRef.current = false
      toggleRef.current?.focus()
    }
    if (diffOpen) wasOpenRef.current = true
  }, [diffOpen])

  useEffect(() => {
    if (!diffOpen) return undefined
    /** Closes the comparison drawer without changing the editor document or review state. */
    function handleWorkspaceKeyDown(event: KeyboardEvent): void {
      if (!isYamlDiffWorkspaceEscapeKey(event.key)) return
      event.preventDefault()
      onDiffOpenChange(false)
    }
    document.addEventListener('keydown', handleWorkspaceKeyDown)
    return () => document.removeEventListener('keydown', handleWorkspaceKeyDown)
  }, [diffOpen, onDiffOpenChange])

  /** Opens or closes the native comparison without touching the editor session. */
  function toggleDiff(): void {
    onDiffOpenChange(!diffOpen)
  }

  return <section className={`yaml-diff-workspace yaml-diff-workspace-${workspaceMode} ${diffOpen ? 'yaml-diff-workspace-open' : 'yaml-diff-workspace-closed'}`} aria-label={accessibility.workspaceLabel}>
    <div className="yaml-diff-workspace-toolbar">
      <button ref={toggleRef} type="button" className="secondary-button yaml-diff-toggle" aria-label={accessibility.toggleLabel} aria-expanded={diffOpen} aria-pressed={diffOpen} aria-controls={YAML_DIFF_WORKSPACE_PANE_ID} onClick={toggleDiff}><FileCode2 size={15} /> {accessibility.toggleLabel}</button>
      <div className="yaml-diff-workspace-status"><span className={`yaml-diff-status yaml-diff-panel-${model.status}`}>{statusIcon}{statusLabel}</span><span>{comparisonAvailable ? 'Native Monaco comparison is ready.' : model.message}</span></div>
    </div>
    <div className="yaml-diff-workspace-body">
      <YamlMonacoEditor baseline={baseline} compareOpen={diffOpen} comparisonAvailable={comparisonAvailable} model={model} onChange={onChange} onCompareOpenChange={onDiffOpenChange} readOnly={readOnly} scopeKey={scopeKey} value={value} workspaceMode={workspaceMode} />
    </div>
  </section>
}

/** Provides the browser viewport fallback used only during server-side element construction. */
const YAML_DIFF_WORKSPACE_DEFAULT_WIDTH = 1024
