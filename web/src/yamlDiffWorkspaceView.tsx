import { AlertTriangle, Check, FileCode2, GripVertical, LoaderCircle, X } from 'lucide-react'
import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent, type ReactNode, type RefObject } from 'react'
import { yamlDiffWorkspaceAccessibility, yamlDiffWorkspaceModeForWidth, clampYamlDiffWorkspaceSplit, createYamlDiffWorkspaceState, isYamlDiffWorkspaceEscapeKey, reduceYamlDiffWorkspaceState, YAML_DIFF_WORKSPACE_PANE_ID, type YamlDiffWorkspaceMode, type YamlDiffWorkspaceState } from './yamlDiffWorkspace'
import { YamlMonacoEditor } from './yamlMonaco'
import type { YamlDiffModel } from './yamlDiff'

/** Defines the editor and live-diff inputs for one integrated YAML workspace. */
export interface YamlDiffWorkspaceProps {
  value: string
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

/** Renders a read-only line-level diff using only the browser-local model. */
function YamlDiffPane({ model, paneID, closeButtonRef, onClose, accessibility }: { model: YamlDiffModel; paneID: string; closeButtonRef: RefObject<HTMLButtonElement>; onClose: () => void; accessibility: ReturnType<typeof yamlDiffWorkspaceAccessibility> }) {
  const statusLabel = yamlDiffStatusLabel(model.status)
  const statusIcon = yamlDiffStatusIcon(model.status)
  const changed = model.status === 'changed'
  return <section id={paneID} className={`yaml-diff-panel yaml-diff-workspace-pane yaml-diff-panel-${model.status}`} aria-label={accessibility.diffPaneLabel} aria-live="polite">
    <div className="yaml-diff-heading">
      <div>
        <span className="eyebrow"><span className="eyebrow-mark" /> Live draft diff</span>
        <strong>Cluster snapshot / local draft</strong>
      </div>
      <div className="yaml-diff-pane-actions">
        <span className="yaml-diff-status">{statusIcon}{statusLabel}</span>
        <button ref={closeButtonRef} type="button" className="icon-button yaml-diff-close" onClick={onClose} aria-label={accessibility.closeLabel} title={accessibility.closeLabel}><X size={15} /></button>
      </div>
    </div>
    <p className="yaml-diff-copy">Read-only, browser-local comparison. It does not review or authorize a mutation.</p>
    {!changed && <div className="yaml-diff-state"><span className="yaml-diff-state-icon">{statusIcon}</span><div><strong>{statusLabel}</strong><span>{model.message}</span></div></div>}
    {changed && <>
      <div className="yaml-diff-summary"><span>{model.message}</span><span>{model.beforeLineCount} snapshot lines / {model.afterLineCount} draft lines</span></div>
      <div className="yaml-diff-desktop" role="table" aria-label="Side-by-side YAML diff">
        <div className="yaml-diff-column-heading" role="row"><span role="columnheader">Cluster snapshot</span><span role="columnheader">Local draft</span></div>
        {model.rows.map((row, index) => <div className={`yaml-diff-row yaml-diff-row-${row.kind}`} role="row" key={`${row.kind}-${index}`}>
          <div className="yaml-diff-cell" role="cell"><span className="yaml-diff-line-number">{row.before?.lineNumber ?? ''}</span><code>{row.before?.text || ' '}</code></div>
          <div className="yaml-diff-cell" role="cell"><span className="yaml-diff-line-number">{row.after?.lineNumber ?? ''}</span><code>{row.after?.text || ' '}</code></div>
        </div>)}
      </div>
      <div className="yaml-diff-unified" aria-label="Unified YAML diff">
        {model.rows.map((row, index) => row.kind === 'changed'
          ? <div className="yaml-diff-unified-change" key={`${row.kind}-${index}`}>
            <div className="yaml-diff-unified-line yaml-diff-unified-removed"><span className="yaml-diff-sign">-</span><span className="yaml-diff-line-number">{row.before?.lineNumber ?? ''}</span><code>{row.before?.text || ' '}</code></div>
            <div className="yaml-diff-unified-line yaml-diff-unified-added"><span className="yaml-diff-sign">+</span><span className="yaml-diff-line-number">{row.after?.lineNumber ?? ''}</span><code>{row.after?.text || ' '}</code></div>
          </div>
          : <div className={`yaml-diff-unified-line yaml-diff-unified-${row.kind}`} key={`${row.kind}-${index}`}><span className="yaml-diff-sign">{row.kind === 'added' ? '+' : row.kind === 'removed' ? '-' : ' '}</span><span className="yaml-diff-line-number">{row.after?.lineNumber ?? row.before?.lineNumber ?? ''}</span><code>{row.after?.text || row.before?.text || ' '}</code></div>)}
      </div>
    </>}
  </section>
}

/** Renders the Monaco editor beside a live diff or beneath its narrow-screen drawer. */
export function YamlDiffWorkspace({ value, readOnly, scopeKey, onChange, model, diffOpen, onDiffOpenChange }: YamlDiffWorkspaceProps) {
  const [workspaceState, setWorkspaceState] = useState<YamlDiffWorkspaceState>(() => createYamlDiffWorkspaceState(diffOpen))
  const workspaceBodyRef = useRef<HTMLDivElement | null>(null)
  const toggleRef = useRef<HTMLButtonElement | null>(null)
  const closeButtonRef = useRef<HTMLButtonElement | null>(null)
  const draggingRef = useRef(false)
  const wasOpenRef = useRef(diffOpen)
  const [workspaceMode, setWorkspaceMode] = useState<YamlDiffWorkspaceMode>(() => yamlDiffWorkspaceModeForWidth(typeof window === 'undefined' ? YAML_DIFF_WORKSPACE_DEFAULT_WIDTH : window.innerWidth))
  const accessibility = yamlDiffWorkspaceAccessibility(diffOpen)

  useEffect(() => {
    /** Keeps the presentation class aligned with the CSS breakpoint after viewport resizing. */
    function handleViewportResize(): void {
      setWorkspaceMode(yamlDiffWorkspaceModeForWidth(window.innerWidth))
    }
    window.addEventListener('resize', handleViewportResize)
    return () => window.removeEventListener('resize', handleViewportResize)
  }, [])

  useEffect(() => {
    setWorkspaceState((current) => reduceYamlDiffWorkspaceState(current, { type: 'set-open', open: diffOpen }))
  }, [diffOpen])

  useEffect(() => {
    if (diffOpen && !wasOpenRef.current) {
      wasOpenRef.current = true
      const focusID = window.requestAnimationFrame(() => closeButtonRef.current?.focus())
      return () => window.cancelAnimationFrame(focusID)
    }
    if (!diffOpen && wasOpenRef.current) {
      wasOpenRef.current = false
      toggleRef.current?.focus()
    }
    return undefined
  }, [diffOpen])

  useEffect(() => {
    if (!diffOpen) return undefined
    /** Closes the narrow diff drawer without changing the editor document or review state. */
    function handleWorkspaceKeyDown(event: KeyboardEvent): void {
      if (!isYamlDiffWorkspaceEscapeKey(event.key)) return
      event.preventDefault()
      onDiffOpenChange(false)
    }
    document.addEventListener('keydown', handleWorkspaceKeyDown)
    return () => document.removeEventListener('keydown', handleWorkspaceKeyDown)
  }, [diffOpen, onDiffOpenChange])

  /** Updates the split from a pointer position while the desktop divider is captured. */
  function handleDividerPointerMove(event: ReactPointerEvent<HTMLDivElement>): void {
    if (!draggingRef.current) return
    const body = workspaceBodyRef.current
    if (!body) return
    const bounds = body.getBoundingClientRect()
    if (!bounds.width) return
    setWorkspaceState((current) => reduceYamlDiffWorkspaceState(current, { type: 'resize', splitPercent: ((event.clientX - bounds.left) / bounds.width) * 100 }))
  }

  /** Starts a pointer resize while retaining the Monaco host in place. */
  function handleDividerPointerDown(event: ReactPointerEvent<HTMLDivElement>): void {
    if (event.button !== 0) return
    draggingRef.current = true
    event.currentTarget.setPointerCapture(event.pointerId)
    event.preventDefault()
  }

  /** Ends a pointer resize without changing editor selection or fold decorations. */
  function handleDividerPointerUp(event: ReactPointerEvent<HTMLDivElement>): void {
    draggingRef.current = false
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
  }

  /** Adjusts the split with keyboard arrows, Home, and End for non-pointer operators. */
  function handleDividerKeyDown(event: ReactKeyboardEvent<HTMLDivElement>): void {
    const current = workspaceState.splitPercent
    const next = event.key === 'ArrowLeft' ? current - 5 : event.key === 'ArrowRight' ? current + 5 : event.key === 'Home' ? 30 : event.key === 'End' ? 70 : undefined
    if (next === undefined) return
    event.preventDefault()
    setWorkspaceState((state) => reduceYamlDiffWorkspaceState(state, { type: 'resize', splitPercent: next }))
  }

  /** Opens or closes the explanatory comparison without touching the editor session. */
  function toggleDiff(): void {
    const nextOpen = !diffOpen
    setWorkspaceState((current) => reduceYamlDiffWorkspaceState(current, { type: 'set-open', open: nextOpen }))
    onDiffOpenChange(nextOpen)
  }

  const workspaceStyle = { '--yaml-diff-editor-split': `${clampYamlDiffWorkspaceSplit(workspaceState.splitPercent)}%` } as CSSProperties
  return <section className={`yaml-diff-workspace yaml-diff-workspace-${workspaceMode} ${diffOpen ? 'yaml-diff-workspace-open' : 'yaml-diff-workspace-closed'}`} aria-label={accessibility.workspaceLabel}>
    <div className="yaml-diff-workspace-toolbar">
      <button ref={toggleRef} type="button" className="secondary-button yaml-diff-toggle" aria-label={accessibility.toggleLabel} aria-expanded={diffOpen} aria-pressed={diffOpen} aria-controls={YAML_DIFF_WORKSPACE_PANE_ID} onClick={toggleDiff}><FileCode2 size={15} /> {accessibility.toggleLabel}</button>
      <span>Compare the local draft with the last synchronized cluster snapshot. This is explanatory only.</span>
    </div>
    <div ref={workspaceBodyRef} className="yaml-diff-workspace-body" style={workspaceStyle}>
      <div className="yaml-diff-editor-pane" role="region" aria-label={accessibility.editorPaneLabel}><YamlMonacoEditor scopeKey={scopeKey} value={value} readOnly={readOnly} onChange={onChange} /></div>
      {diffOpen && <>
        <div className="yaml-diff-divider" role="separator" aria-label={accessibility.dividerLabel} aria-orientation="vertical" aria-valuemin={30} aria-valuemax={70} aria-valuenow={Math.round(workspaceState.splitPercent)} aria-valuetext={`Editor ${Math.round(workspaceState.splitPercent)} percent; diff ${Math.round(100 - workspaceState.splitPercent)} percent`} tabIndex={0} onPointerDown={handleDividerPointerDown} onPointerMove={handleDividerPointerMove} onPointerUp={handleDividerPointerUp} onPointerCancel={handleDividerPointerUp} onKeyDown={handleDividerKeyDown} title={accessibility.dividerLabel}><GripVertical size={15} /></div>
        <button type="button" className="yaml-diff-drawer-scrim" onClick={() => onDiffOpenChange(false)} aria-label={accessibility.closeLabel} tabIndex={-1} />
        <YamlDiffPane model={model} paneID={YAML_DIFF_WORKSPACE_PANE_ID} closeButtonRef={closeButtonRef} onClose={() => onDiffOpenChange(false)} accessibility={accessibility} />
      </>}
    </div>
  </section>
}

/** Provides the browser viewport fallback used only during server-side element construction. */
const YAML_DIFF_WORKSPACE_DEFAULT_WIDTH = 1024
