import * as monaco from 'monaco-editor/editor/editor.api'
import EditorWorker from 'monaco-editor/editor/editor.worker?worker'
import { configureMonacoYaml, type MonacoYaml } from 'monaco-yaml'
import { AlertTriangle, Check, FileCode2, LoaderCircle, X } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import YamlWorker from './yaml.worker?worker'
import { yamlDiffHunks, type YamlDiffHunk, type YamlDiffModel } from './yamlDiff'
import { yamlDiffWorkspaceNativeComparisonAvailable, YAML_DIFF_WORKSPACE_PANE_ID, type YamlDiffWorkspaceMode } from './yamlDiffWorkspace'
import {
  yamlEditorDocumentChange,
  yamlMonacoComparisonInitializationError,
  yamlMonacoEditorOptions,
  yamlMonacoHunkModelUri,
  yamlMonacoHunkPeekInitializationError,
  yamlMonacoInitializationError,
  yamlMonacoModelUri,
  yamlMonacoOriginalModelUri,
  yamlMonacoWorkerLabelSupported,
} from './yamlMonacoState'
import 'monaco-editor/languages/definitions/yaml/register'

/** Identifies the constructor shape emitted by Vite for a browser worker module. */
type MonacoWorkerConstructor = new () => Worker

/** Identifies the callback used when a live Monaco worker reports a startup failure. */
type MonacoWorkerFailureHandler = () => void

/** Describes the browser-global worker hook consumed by Monaco and monaco-yaml. */
interface MonacoWorkerEnvironment {
  getWorker: (moduleId: string, label: string) => Worker
  [key: string]: unknown
}

/** Owns the original and modified text models for one resource-scoped editor session. */
interface YamlMonacoModels {
  original: monaco.editor.ITextModel
  modified: monaco.editor.ITextModel
}

/** Owns one temporary nested Monaco diff editor and its hunk-only models. */
interface YamlMonacoHunkPeekSession {
  dispose: () => void
}

/** Defines the dark peat, copper, moss, and monospace Monaco visual language. */
const KUBE_HAGGIS_MONACO_THEME = 'kube-haggis-yaml'

/** Holds the single configured YAML language service required by Monaco YAML instances. */
let yamlLanguageService: MonacoYaml | undefined

/** Tracks whether both required worker constructors have passed their browser probe. */
let monacoWorkersConfigured = false

/** Holds the active editor lifecycle callback for asynchronous worker failures. */
let monacoWorkerFailureHandler: MonacoWorkerFailureHandler | undefined

/** Creates a worker and verifies the browser methods required by Monaco are present. */
function createVerifiedMonacoWorker(WorkerConstructor: MonacoWorkerConstructor, onFailure = monacoWorkerFailureHandler): Worker {
  const worker = new WorkerConstructor()
  if (typeof worker.postMessage !== 'function' || typeof worker.terminate !== 'function') {
    worker.terminate?.()
    throw new Error('The Monaco worker did not expose the required browser API.')
  }
  const reportFailure = (): void => onFailure?.()
  worker.addEventListener('error', reportFailure)
  worker.addEventListener('messageerror', reportFailure)
  return worker
}

/** Installs and probes the local Monaco and YAML worker constructors before editor creation. */
export function configureYamlMonacoWorkers(onFailure?: MonacoWorkerFailureHandler): void {
  monacoWorkerFailureHandler = onFailure
  if (monacoWorkersConfigured) return

  const globalObject = globalThis as typeof globalThis & { MonacoEnvironment?: MonacoWorkerEnvironment }
  const previousEnvironment = globalObject.MonacoEnvironment
  const getWorker = (_moduleId: string, label: string): Worker => {
    if (label === 'yaml') return createVerifiedMonacoWorker(YamlWorker)
    if (label === 'editorWorkerService') return createVerifiedMonacoWorker(EditorWorker)
    if (!yamlMonacoWorkerLabelSupported(label)) throw new Error(`Unsupported Monaco worker label: ${label}`)
    throw new Error(`Monaco worker label is not configured: ${label}`)
  }
  const nextEnvironment: MonacoWorkerEnvironment = { ...(previousEnvironment || {}), getWorker }
  globalObject.MonacoEnvironment = nextEnvironment

  try {
    const editorWorker = getWorker('kube-haggis', 'editorWorkerService')
    const yamlWorker = getWorker('monaco-yaml/yaml.worker', 'yaml')
    editorWorker.terminate()
    yamlWorker.terminate()
    monacoWorkersConfigured = true
  } catch (error) {
    globalObject.MonacoEnvironment = previousEnvironment
    throw error
  }
}

/** Defines the dark diff indicators and editor colors used by every native YAML surface. */
function defineYamlMonacoTheme(): void {
  monaco.editor.defineTheme(KUBE_HAGGIS_MONACO_THEME, {
    base: 'vs-dark',
    inherit: true,
    colors: {
      'editor.background': '#0b0e0b',
      'editor.foreground': '#e8e7de',
      'editorLineNumber.foreground': '#697265',
      'editorLineNumber.activeForeground': '#f09a62',
      'editorGutter.background': '#0b0e0b',
      'editor.lineHighlightBackground': '#171b1680',
      'editor.selectionBackground': '#d77b4d3d',
      'editor.inactiveSelectionBackground': '#d77b4d24',
      'editorCursor.foreground': '#f09a62',
      'editorWhitespace.foreground': '#69726580',
      'editorIndentGuide.background': '#e2e0cc12',
      'editorIndentGuide.activeBackground': '#d77b4d4d',
      'editorOverviewRuler.border': '#00000000',
      'editorError.foreground': '#e6b66f',
      'editorWarning.foreground': '#e6b66f',
      'editorInfo.foreground': '#87aeb6',
      'editorBracketMatch.background': '#d77b4d24',
      'editorBracketMatch.border': '#f09a6266',
      'editor.foldBackground': '#d77b4d12',
      'editorGutter.foldingControlForeground': '#697265',
      'editorGutter.addedBackground': '#a8bd83',
      'editorGutter.modifiedBackground': '#f09a62',
      'editorGutter.deletedBackground': '#d77b4d',
      'diffEditor.insertedTextBackground': '#5b774a3d',
      'diffEditor.insertedLineBackground': '#5b774a26',
      'diffEditor.removedTextBackground': '#7e4b2c3d',
      'diffEditor.removedLineBackground': '#7e4b2c26',
      'diffEditor.diagonalFill': '#69726526',
      'diffEditor.border': '#e2e0cc18',
    },
    rules: [
      { token: 'comment', foreground: '697265' },
      { token: 'type', foreground: 'f09a62' },
      { token: 'operators', foreground: 'd77b4d' },
      { token: 'string', foreground: 'a8bd83' },
      { token: 'number', foreground: 'e6b66f' },
      { token: 'keyword', foreground: 'e6b66f' },
      { token: 'tag', foreground: '87aeb6' },
      { token: 'metatag', foreground: 'd7d6c8' },
      { token: 'delimiter', foreground: '8e9687' },
    ],
  })
}

/** Configures the browser-local YAML language service without requesting remote schemas. */
function configureYamlLanguage(): void {
  if (yamlLanguageService) return
  yamlLanguageService = configureMonacoYaml(monaco, {
    enableSchemaRequest: false,
    format: { enable: false },
    isKubernetes: true,
    validate: true,
  })
}

/** Disposes the shared YAML language worker manager when the editor scope leaves the page. */
function disposeYamlLanguage(): void {
  const service = yamlLanguageService as (MonacoYaml & { dispose?: () => void }) | undefined
  service?.dispose?.()
  yamlLanguageService = undefined
}

/** Disposes the native comparison while returning its modified view state to the main editor. */
function disposeYamlMonacoDiffEditor(
  diffEditorRef: { current: monaco.editor.IStandaloneDiffEditor | null },
  mainEditorRef: { current: monaco.editor.IStandaloneCodeEditor | null },
): void {
  const diffEditor = diffEditorRef.current
  if (!diffEditor) return
  const modifiedViewState = diffEditor.getModifiedEditor().saveViewState()
  if (modifiedViewState && mainEditorRef.current) mainEditorRef.current.restoreViewState(modifiedViewState)
  diffEditor.setModel(null)
  diffEditor.dispose()
  diffEditorRef.current = null
}

/** Disposes the Monaco editor, listeners, models, and temporary comparison state for one scope. */
function disposeYamlMonacoSession(
  mainEditorRef: { current: monaco.editor.IStandaloneCodeEditor | null },
  diffEditorRef: { current: monaco.editor.IStandaloneDiffEditor | null },
  modelsRef: { current: YamlMonacoModels | null },
  listenerRef: { current: monaco.IDisposable[] },
): void {
  disposeYamlMonacoDiffEditor(diffEditorRef, mainEditorRef)
  for (const listener of listenerRef.current) listener.dispose()
  listenerRef.current = []
  mainEditorRef.current?.dispose()
  mainEditorRef.current = null
  modelsRef.current?.original.dispose()
  modelsRef.current?.modified.dispose()
  modelsRef.current = null
}

/** Captures selections as model offsets so a parent synchronization does not move the caret. */
function captureYamlMonacoSelections(editor: monaco.editor.ICodeEditor, model: monaco.editor.ITextModel): Array<{ anchor: number; active: number }> {
  return (editor.getSelections() || []).map((selection) => ({
    anchor: model.getOffsetAt({ lineNumber: selection.selectionStartLineNumber, column: selection.selectionStartColumn }),
    active: model.getOffsetAt({ lineNumber: selection.positionLineNumber, column: selection.positionColumn }),
  }))
}

/** Restores captured model-offset selections after replacing the complete external document. */
function restoreYamlMonacoSelections(editor: monaco.editor.ICodeEditor, model: monaco.editor.ITextModel, selections: Array<{ anchor: number; active: number }>): void {
  const length = model.getValueLength()
  editor.setSelections(selections.map((selection) => {
    const anchor = model.getPositionAt(Math.min(selection.anchor, length))
    const active = model.getPositionAt(Math.min(selection.active, length))
    return new monaco.Selection(anchor.lineNumber, anchor.column, active.lineNumber, active.column)
  }))
}

/** Returns the hunk containing a modified-side line or no hunk when the line is unchanged. */
function yamlMonacoHunkAtLine(hunks: YamlDiffHunk[], lineNumber: number): YamlDiffHunk | undefined {
  return hunks.find((hunk) => lineNumber >= hunk.modifiedStartLineNumber && lineNumber <= hunk.modifiedEndLineNumber)
}

/** Extracts one bounded hunk source range without adding content to non-editor surfaces. */
function yamlMonacoHunkText(model: monaco.editor.ITextModel, startLineNumber: number, endLineNumber: number): string {
  const start = Math.min(Math.max(1, startLineNumber), model.getLineCount())
  const end = Math.min(Math.max(start, endLineNumber), model.getLineCount())
  return model.getValueInRange(new monaco.Range(start, 1, end, model.getLineMaxColumn(end)))
}

/** Creates and owns a read-only Monaco diff widget for one selected YAML hunk. */
function createYamlMonacoHunkPeek(
  editor: monaco.editor.IStandaloneCodeEditor,
  models: YamlMonacoModels,
  hunk: YamlDiffHunk,
  scopeKey: string,
  onClose: () => void,
): YamlMonacoHunkPeekSession {
  const root = document.createElement('div')
  root.className = 'yaml-monaco-hunk-peek'
  root.setAttribute('role', 'dialog')
  root.setAttribute('aria-label', 'Read-only YAML change peek')

  const heading = document.createElement('div')
  heading.className = 'yaml-monaco-hunk-peek-heading'
  const title = document.createElement('strong')
  title.textContent = 'Read-only change peek'
  const closeButton = document.createElement('button')
  closeButton.type = 'button'
  closeButton.className = 'icon-button yaml-monaco-hunk-peek-close'
  closeButton.setAttribute('aria-label', 'Close YAML change peek')
  closeButton.title = 'Close YAML change peek'
  closeButton.textContent = 'x'
  heading.append(title, closeButton)

  const summary = document.createElement('span')
  summary.className = 'yaml-monaco-hunk-peek-summary'
  summary.textContent = `${hunk.kind} hunk - editor remains editable`
  const host = document.createElement('div')
  host.className = 'yaml-monaco-hunk-peek-host'
  root.append(heading, summary, host)

  const originalText = hunk.kind === 'added' ? '' : yamlMonacoHunkText(models.original, hunk.originalStartLineNumber, hunk.originalEndLineNumber)
  const modifiedText = hunk.kind === 'removed' ? '' : yamlMonacoHunkText(models.modified, hunk.modifiedStartLineNumber, hunk.modifiedEndLineNumber)
  const originalUri = monaco.Uri.parse(yamlMonacoHunkModelUri(scopeKey, hunk.id, 'original'))
  const modifiedUri = monaco.Uri.parse(yamlMonacoHunkModelUri(scopeKey, hunk.id, 'modified'))
  monaco.editor.getModel(originalUri)?.dispose()
  monaco.editor.getModel(modifiedUri)?.dispose()
  let originalModel: monaco.editor.ITextModel | undefined
  let modifiedModel: monaco.editor.ITextModel | undefined
  let peekEditor: monaco.editor.IStandaloneDiffEditor | undefined
  let widget: monaco.editor.IContentWidget | undefined
  let widgetAdded = false
  let layoutFrameID: number | undefined

  /** Closes the hunk peek on Escape without changing the underlying Monaco model. */
  function handlePeekKeyDown(event: KeyboardEvent): void {
    if (event.key !== 'Escape') return
    event.preventDefault()
    event.stopPropagation()
    onClose()
  }

  try {
    originalModel = monaco.editor.createModel(originalText, 'yaml', originalUri)
    modifiedModel = monaco.editor.createModel(modifiedText, 'yaml', modifiedUri)
    peekEditor = monaco.editor.createDiffEditor(host, {
      ...yamlMonacoEditorOptions(true),
      accessibilityVerbose: true,
      ariaLabel: 'Read-only YAML change peek',
      automaticLayout: true,
      diffWordWrap: 'off',
      enableSplitViewResizing: false,
      fontFamily: "'DM Mono', 'SFMono-Regular', Consolas, monospace",
      fontSize: 11,
      lineHeight: 18,
      lineNumbers: 'on',
      minimap: { enabled: false },
      modifiedAriaLabel: 'Read-only modified YAML hunk',
      originalAriaLabel: 'Read-only original YAML hunk',
      originalEditable: false,
      padding: { top: 8, bottom: 10 },
      renderIndicators: true,
      renderMarginRevertIcon: false,
      renderSideBySide: true,
      scrollBeyondLastLine: false,
      theme: KUBE_HAGGIS_MONACO_THEME,
      useInlineViewWhenSpaceIsLimited: false,
      wordWrap: 'off',
    })
    peekEditor.setModel({ original: originalModel, modified: modifiedModel })

    widget = {
      allowEditorOverflow: true,
      beforeRender: () => ({ width: 640, height: 205 }),
      getDomNode: () => root,
      getId: () => `kube-haggis-yaml-hunk-peek-${hunk.id}`,
      getPosition: () => ({
        position: { lineNumber: hunk.modifiedStartLineNumber, column: 1 },
        preference: [monaco.editor.ContentWidgetPositionPreference.BELOW, monaco.editor.ContentWidgetPositionPreference.ABOVE],
      }),
      suppressMouseDown: false,
    }
    editor.addContentWidget(widget)
    widgetAdded = true
    closeButton.addEventListener('click', onClose)

    window.addEventListener('keydown', handlePeekKeyDown)

    /** Lays out the temporary diff after Monaco attaches the content widget to the DOM. */
    const layoutPeek = (): void => {
      if (!peekEditor) return
      peekEditor.layout({ width: host.clientWidth || 640, height: host.clientHeight || 174 })
    }
    layoutFrameID = window.requestAnimationFrame(layoutPeek)
    return {
      dispose: () => {
        closeButton.removeEventListener('click', onClose)
        window.removeEventListener('keydown', handlePeekKeyDown)
        if (layoutFrameID !== undefined) window.cancelAnimationFrame(layoutFrameID)
        if (widgetAdded && widget) editor.removeContentWidget(widget)
        peekEditor?.setModel(null)
        peekEditor?.dispose()
        originalModel?.dispose()
        modifiedModel?.dispose()
        root.remove()
      },
    }
  } catch (error) {
    window.removeEventListener('keydown', handlePeekKeyDown)
    if (layoutFrameID !== undefined) window.cancelAnimationFrame(layoutFrameID)
    if (widgetAdded && widget) editor.removeContentWidget(widget)
    peekEditor?.setModel(null)
    peekEditor?.dispose()
    originalModel?.dispose()
    modifiedModel?.dispose()
    root.remove()
    throw error
  }
}

/** Describes the raw draft and native comparison state for one resource-scoped Monaco workspace. */
export interface YamlMonacoEditorProps {
  value: string
  baseline: string
  readOnly: boolean
  scopeKey: string
  model: YamlDiffModel
  compareOpen: boolean
  comparisonAvailable: boolean
  workspaceMode: YamlDiffWorkspaceMode
  onChange: (value: string) => void
  onCompareOpenChange: (open: boolean) => void
}

/** Returns the operator-facing label for the bounded native comparison state. */
function yamlMonacoDiffStatusLabel(status: YamlDiffModel['status']): string {
  if (status === 'clean') return 'Clean'
  if (status === 'changed') return 'Draft differs'
  if (status === 'invalid') return 'Incomplete draft'
  if (status === 'bounded') return 'Bounded'
  if (status === 'computing') return 'Calculating'
  return 'No snapshot'
}

/** Returns a safe status icon for the native comparison header. */
function yamlMonacoDiffStatusIcon(status: YamlDiffModel['status']): ReactNode {
  if (status === 'clean') return <Check size={14} />
  if (status === 'computing') return <LoaderCircle size={14} className="spin" />
  return <AlertTriangle size={14} />
}

/** Renders the resource-scoped standalone editor, native diff editor, and hunk peek lifecycle. */
export function YamlMonacoEditor({ value, baseline, readOnly, scopeKey, model, compareOpen, comparisonAvailable, workspaceMode, onChange, onCompareOpenChange }: YamlMonacoEditorProps) {
  const mainHostRef = useRef<HTMLDivElement | null>(null)
  const diffHostRef = useRef<HTMLDivElement | null>(null)
  const mainEditorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null)
  const diffEditorRef = useRef<monaco.editor.IStandaloneDiffEditor | null>(null)
  const modelsRef = useRef<YamlMonacoModels | null>(null)
  const listenerRef = useRef<monaco.IDisposable[]>([])
  const peekSessionRef = useRef<YamlMonacoHunkPeekSession | null>(null)
  const decorationIDsRef = useRef<string[]>([])
  const nativeComparisonReadyRef = useRef(false)
  const syncingRef = useRef(false)
  const onChangeRef = useRef(onChange)
  const markerHunksRef = useRef<YamlDiffHunk[]>([])
  const compareCloseButtonRef = useRef<HTMLButtonElement | null>(null)
  const [ready, setReady] = useState(false)
  const [initializationError, setInitializationError] = useState('')
  const [comparisonError, setComparisonError] = useState('')
  const [peekError, setPeekError] = useState('')
  const [activeHunkID, setActiveHunkID] = useState<string | undefined>()

  const hunks = yamlDiffHunks(model)
  markerHunksRef.current = hunks
  const activeHunk = hunks.find((hunk) => hunk.id === activeHunkID)
  const activeHunkKey = activeHunk
    ? `${activeHunk.id}|${activeHunk.originalStartLineNumber}|${activeHunk.originalEndLineNumber}|${activeHunk.modifiedStartLineNumber}|${activeHunk.modifiedEndLineNumber}`
    : ''
  const nativeComparisonAvailable = yamlDiffWorkspaceNativeComparisonAvailable(comparisonAvailable, model.status, compareOpen, nativeComparisonReadyRef.current)

  useEffect(() => {
    onChangeRef.current = onChange
  }, [onChange])

  /** Closes the current read-only hunk peek without changing the draft or review state. */
  function closeHunkPeek(): void {
    setActiveHunkID(undefined)
    setPeekError('')
  }

  /** Selects or toggles a marker hunk while leaving the editable Monaco session untouched. */
  function selectHunk(hunkID: string): void {
    setActiveHunkID((current) => current === hunkID ? undefined : hunkID)
    setPeekError('')
  }

  /** Closes Compare from its native pane and returns focus to the Compare action. */
  function closeComparison(): void {
    onCompareOpenChange(false)
  }

  useLayoutEffect(() => {
    let active = true
    const host = mainHostRef.current
    setReady(false)
    setInitializationError('')
    setComparisonError('')
    if (!host) {
      setInitializationError(yamlMonacoInitializationError(new Error('The Monaco host element was unavailable.')))
      setComparisonError(yamlMonacoComparisonInitializationError(new Error('The Monaco host element was unavailable.')))
      return () => {
        active = false
      }
    }

    const handleWorkerFailure = (): void => {
      if (!active) return
      peekSessionRef.current?.dispose()
      peekSessionRef.current = null
      disposeYamlMonacoSession(mainEditorRef, diffEditorRef, modelsRef, listenerRef)
      setReady(false)
      setInitializationError(yamlMonacoInitializationError(new Error('A required Monaco worker stopped.')))
      setComparisonError(yamlMonacoComparisonInitializationError(new Error('A required Monaco worker stopped.')))
    }

    try {
      configureYamlMonacoWorkers(handleWorkerFailure)
      configureYamlLanguage()
      defineYamlMonacoTheme()
      const modifiedURI = monaco.Uri.parse(yamlMonacoModelUri(scopeKey))
      const originalURI = monaco.Uri.parse(yamlMonacoOriginalModelUri(scopeKey))
      monaco.editor.getModel(modifiedURI)?.dispose()
      monaco.editor.getModel(originalURI)?.dispose()
      const modified = monaco.editor.createModel(value, 'yaml', modifiedURI)
      const original = monaco.editor.createModel(baseline, 'yaml', originalURI)
      const editor = monaco.editor.create(host, {
        ...yamlMonacoEditorOptions(readOnly),
        ariaLabel: 'Editable local-draft YAML',
        fontFamily: "'DM Mono', 'SFMono-Regular', Consolas, monospace",
        fontSize: 12,
        lineHeight: 19,
        lineNumbers: 'on',
        minimap: { enabled: false },
        model: modified,
        overviewRulerBorder: false,
        padding: { top: 14, bottom: 18 },
        renderLineHighlight: 'all',
        renderWhitespace: 'selection',
        scrollBeyondLastLine: false,
        selectOnLineNumbers: true,
        tabSize: 2,
        theme: KUBE_HAGGIS_MONACO_THEME,
        wordWrap: 'off',
      })
      if (!active) {
        editor.dispose()
        original.dispose()
        modified.dispose()
        return () => undefined
      }

      modelsRef.current = { original, modified }
      mainEditorRef.current = editor
      const contentListener = modified.onDidChangeContent(() => {
        if (!syncingRef.current) onChangeRef.current(modified.getValue())
      })
      const mouseListener = editor.onMouseDown((event) => {
        if (event.target.type !== monaco.editor.MouseTargetType.GUTTER_GLYPH_MARGIN) return
        const lineNumber = event.target.position?.lineNumber
        if (!lineNumber) return
        const hunk = yamlMonacoHunkAtLine(markerHunksRef.current, lineNumber)
        if (!hunk) return
        event.event.preventDefault()
        event.event.stopPropagation()
        selectHunk(hunk.id)
      })
      const action = editor.addAction({
        id: 'kube-haggis.yaml.open-change-peek',
        keybindings: [monaco.KeyMod.Alt | monaco.KeyCode.DownArrow],
        label: 'Open YAML change peek',
        run: (targetEditor) => {
          const lineNumber = targetEditor.getPosition()?.lineNumber
          const hunk = lineNumber ? yamlMonacoHunkAtLine(markerHunksRef.current, lineNumber) : undefined
          if (hunk) selectHunk(hunk.id)
        },
      })
      listenerRef.current = [contentListener, mouseListener, action]
      setReady(true)
    } catch (error) {
      disposeYamlMonacoSession(mainEditorRef, diffEditorRef, modelsRef, listenerRef)
      if (active) {
        setInitializationError(yamlMonacoInitializationError(error))
        setComparisonError(yamlMonacoComparisonInitializationError(error))
      }
    }

    return () => {
      active = false
      if (monacoWorkerFailureHandler === handleWorkerFailure) monacoWorkerFailureHandler = undefined
      peekSessionRef.current?.dispose()
      peekSessionRef.current = null
      disposeYamlMonacoSession(mainEditorRef, diffEditorRef, modelsRef, listenerRef)
      disposeYamlLanguage()
    }
  }, [scopeKey])

  useLayoutEffect(() => {
    const models = modelsRef.current
    if (!models || models.original.getValue() === baseline) return
    models.original.applyEdits([{ range: models.original.getFullModelRange(), text: baseline }])
  }, [baseline, ready])

  useLayoutEffect(() => {
    const models = modelsRef.current
    if (!models || models.modified.getValue() === value) return
    const activeEditor = diffEditorRef.current?.getModifiedEditor() || mainEditorRef.current
    const selections = activeEditor ? captureYamlMonacoSelections(activeEditor, models.modified) : []
    syncingRef.current = true
    try {
      models.modified.applyEdits([{ range: models.modified.getFullModelRange(), text: yamlEditorDocumentChange(models.modified.getValue(), value)?.insert || value }])
      if (activeEditor && selections.length) restoreYamlMonacoSelections(activeEditor, models.modified, selections)
      if (mainEditorRef.current && mainEditorRef.current !== activeEditor && selections.length) restoreYamlMonacoSelections(mainEditorRef.current, models.modified, selections)
    } finally {
      syncingRef.current = false
    }
  }, [value, ready])

  useEffect(() => {
    const mainEditor = mainEditorRef.current
    if (!mainEditor) return
    const decorations = hunks.map((hunk) => ({
      range: new monaco.Range(hunk.modifiedStartLineNumber, 1, hunk.modifiedEndLineNumber, 1),
      options: {
        glyphMargin: { position: monaco.editor.GlyphMarginLane.Left },
        glyphMarginClassName: `yaml-diff-marker yaml-diff-marker-${hunk.kind}`,
        glyphMarginHoverMessage: { value: 'Open read-only YAML change peek' },
        isWholeLine: true,
        linesDecorationsClassName: `yaml-diff-line-marker yaml-diff-line-marker-${hunk.kind}`,
        overviewRuler: { color: hunk.kind === 'removed' ? '#d77b4d' : hunk.kind === 'added' ? '#a8bd83' : '#f09a62', position: monaco.editor.OverviewRulerLane.Right },
        showIfCollapsed: true,
      },
    }))
    decorationIDsRef.current = mainEditor.deltaDecorations(decorationIDsRef.current, decorations)
  }, [model, ready])

  useLayoutEffect(() => {
    if (!compareOpen || !nativeComparisonAvailable) {
      disposeYamlMonacoDiffEditor(diffEditorRef, mainEditorRef)
      nativeComparisonReadyRef.current = false
      if (!compareOpen) setComparisonError('')
      return () => undefined
    }
    const host = diffHostRef.current
    const models = modelsRef.current
    const mainEditor = mainEditorRef.current
    if (!host || !models || !mainEditor) return () => undefined
    setComparisonError('')
    try {
      const diffEditor = monaco.editor.createDiffEditor(host, {
        ...yamlMonacoEditorOptions(readOnly),
        accessibilityVerbose: true,
        ariaLabel: 'Native YAML comparison',
        diffAlgorithm: 'advanced',
        diffWordWrap: 'off',
        enableSplitViewResizing: true,
        fontFamily: "'DM Mono', 'SFMono-Regular', Consolas, monospace",
        fontSize: 12,
        lineHeight: 19,
        lineNumbers: 'on',
        minimap: { enabled: false },
        modifiedAriaLabel: 'Editable modified local-draft YAML',
        originalAriaLabel: 'Read-only original cluster YAML',
        originalEditable: false,
        padding: { top: 14, bottom: 18 },
        renderIndicators: true,
        renderMarginRevertIcon: false,
        renderOverviewRuler: true,
        renderSideBySide: true,
        scrollBeyondLastLine: false,
        splitViewDefaultRatio: 0.5,
        theme: KUBE_HAGGIS_MONACO_THEME,
        useInlineViewWhenSpaceIsLimited: false,
        wordWrap: 'off',
      })
      diffEditor.setModel({ original: models.original, modified: models.modified })
      const mainViewState = mainEditor.saveViewState()
      if (mainViewState) diffEditor.restoreViewState({ original: null, modified: mainViewState, modelState: undefined })
      diffEditorRef.current = diffEditor
      nativeComparisonReadyRef.current = true
    } catch (error) {
      disposeYamlMonacoDiffEditor(diffEditorRef, mainEditorRef)
      nativeComparisonReadyRef.current = false
      setComparisonError(yamlMonacoComparisonInitializationError(error))
    }
    return () => {
      disposeYamlMonacoDiffEditor(diffEditorRef, mainEditorRef)
      nativeComparisonReadyRef.current = false
    }
  }, [compareOpen, nativeComparisonAvailable, ready])

  useEffect(() => {
    if (compareOpen) closeHunkPeek()
  }, [compareOpen])

  useLayoutEffect(() => {
    if (!activeHunk || compareOpen || !ready) return () => undefined
    const editor = mainEditorRef.current
    const models = modelsRef.current
    if (!editor || !models) return () => undefined
    peekSessionRef.current?.dispose()
    peekSessionRef.current = null
    setPeekError('')
    try {
      peekSessionRef.current = createYamlMonacoHunkPeek(editor, models, activeHunk, scopeKey, closeHunkPeek)
    } catch (error) {
      setPeekError(yamlMonacoHunkPeekInitializationError(error))
    }
    return () => {
      peekSessionRef.current?.dispose()
      peekSessionRef.current = null
    }
  }, [activeHunkKey, baseline, compareOpen, model, ready, scopeKey])

  useEffect(() => {
    if (!activeHunkID || activeHunk) return
    closeHunkPeek()
  }, [activeHunk, activeHunkID])

  useEffect(() => {
    const mainEditor = mainEditorRef.current
    const diffEditor = diffEditorRef.current
    mainEditor?.updateOptions({ readOnly, domReadOnly: readOnly })
    diffEditor?.updateOptions({ readOnly, domReadOnly: readOnly })
    diffEditor?.getModifiedEditor().updateOptions({ readOnly, domReadOnly: readOnly })
  }, [readOnly])

  useEffect(() => {
    if (!compareOpen) return undefined
    const frameID = window.requestAnimationFrame(() => compareCloseButtonRef.current?.focus())
    return () => window.cancelAnimationFrame(frameID)
  }, [compareOpen])

  const editorClass = [
    'yaml-monaco-editor',
    `yaml-monaco-editor-${workspaceMode}`,
    compareOpen ? 'yaml-monaco-editor-compare-open' : 'yaml-monaco-editor-compare-closed',
    nativeComparisonAvailable ? '' : 'yaml-monaco-editor-comparison-unavailable',
    comparisonError ? 'yaml-monaco-editor-comparison-error' : '',
    initializationError ? 'yaml-monaco-editor-initialization-error' : '',
  ].filter(Boolean).join(' ')
  const statusLabel = yamlMonacoDiffStatusLabel(model.status)
  const statusIcon = yamlMonacoDiffStatusIcon(model.status)

  return <div className={editorClass}>
    <div ref={mainHostRef} className={`yaml-monaco-host yaml-monaco-main-host${initializationError ? ' yaml-monaco-host-blocked' : ''}`} aria-label="Editable local-draft YAML" />
    {compareOpen && <button type="button" className="yaml-monaco-compare-scrim" onClick={closeComparison} aria-label="Close YAML comparison" tabIndex={-1} />}
    {compareOpen && <section id={YAML_DIFF_WORKSPACE_PANE_ID} className="yaml-monaco-compare-pane" aria-label="Read-only original and editable modified YAML comparison" aria-live="polite">
      <div className="yaml-monaco-compare-heading">
        <div>
          <span className="eyebrow"><span className="eyebrow-mark" /> Native comparison</span>
          <strong>Cluster snapshot / local draft</strong>
        </div>
        <div className="yaml-monaco-compare-actions">
          <span className={`yaml-diff-status yaml-diff-panel-${model.status}`}>{statusIcon}{statusLabel}</span>
          <button ref={compareCloseButtonRef} type="button" className="icon-button yaml-monaco-compare-close" onClick={closeComparison} aria-label="Close YAML comparison" title="Close YAML comparison"><X size={15} /></button>
        </div>
      </div>
      <p className="yaml-monaco-compare-copy">The original pane is read-only. The modified pane is the local draft source of truth; server-side dry-run review still authorizes mutations.</p>
      {comparisonError
        ? <div className="yaml-monaco-compare-state yaml-monaco-compare-state-error" role="alert"><AlertTriangle size={17} /><div><strong>Native comparison unavailable</strong><span>{comparisonError}</span></div></div>
        : !nativeComparisonAvailable
          ? <div className="yaml-monaco-compare-state" role="status"><span className="yaml-diff-state-icon">{statusIcon}</span><div><strong>{statusLabel}</strong><span>{model.message}</span></div></div>
          : <div ref={diffHostRef} className="yaml-monaco-diff-host" aria-label="Monaco side-by-side YAML comparison" />}
    </section>}
    {peekError && <div className="yaml-monaco-peek-error" role="alert"><AlertTriangle size={14} /><span>{peekError}</span></div>}
    {initializationError
      ? <div className="yaml-editor-error" role="alert"><AlertTriangle size={16} /><div><strong>Monaco YAML editor unavailable</strong><span>{initializationError}</span></div></div>
      : !ready && <div className="yaml-editor-notice" role="status"><LoaderCircle size={14} className="spin" /><span>Starting the local Monaco YAML editor and required workers.</span></div>}
    {!initializationError && ready && !compareOpen && model.status === 'changed' && <div className="yaml-monaco-marker-help" role="status"><FileCode2 size={13} /><span>Change markers are in the gutter. Click one, or use Alt+Down on a changed line, to open a read-only hunk peek.</span></div>}
  </div>
}
