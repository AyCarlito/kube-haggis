import * as monaco from 'monaco-editor/editor/editor.api'
import EditorWorker from 'monaco-editor/editor/editor.worker?worker'
import { configureMonacoYaml, type MonacoYaml } from 'monaco-yaml'
import { AlertTriangle, LoaderCircle } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import YamlWorker from './yaml.worker?worker'
import {
  yamlEditorDocumentChange,
  yamlMonacoEditorOptions,
  yamlMonacoInitializationError,
  yamlMonacoModelUri,
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

/** Defines the stable KubeHaggis theme name used by every Monaco YAML instance. */
const KUBE_HAGGIS_MONACO_THEME = 'kube-haggis-yaml'

/** Holds the single configured YAML language service required by monaco-yaml. */
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

/** Defines the dark peat, copper, moss, and monospace Monaco visual language. */
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

/** Releases the current editor, model, and content listener as one lifecycle unit. */
function disposeYamlMonacoSession(
  editorRef: { current: monaco.editor.IStandaloneCodeEditor | null },
  modelRef: { current: monaco.editor.ITextModel | null },
  changeListenerRef: { current: monaco.IDisposable | null },
): void {
  changeListenerRef.current?.dispose()
  changeListenerRef.current = null
  editorRef.current?.dispose()
  editorRef.current = null
  modelRef.current?.dispose()
  modelRef.current = null
}

/** Defines the raw-draft inputs and resource scope for the Monaco YAML editor. */
export interface YamlMonacoEditorProps {
  value: string
  readOnly: boolean
  scopeKey: string
  onChange: (value: string) => void
}

/** Renders a browser-local Monaco YAML editor with native indentation folding and no fallback editor. */
export function YamlMonacoEditor({ value, readOnly, scopeKey, onChange }: YamlMonacoEditorProps) {
  const hostRef = useRef<HTMLDivElement | null>(null)
  const editorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null)
  const modelRef = useRef<monaco.editor.ITextModel | null>(null)
  const changeListenerRef = useRef<monaco.IDisposable | null>(null)
  const syncingRef = useRef(false)
  const onChangeRef = useRef(onChange)
  const [ready, setReady] = useState(false)
  const [initializationError, setInitializationError] = useState('')

  useEffect(() => {
    onChangeRef.current = onChange
  }, [onChange])

  useLayoutEffect(() => {
    let active = true
    const host = hostRef.current
    setReady(false)
    setInitializationError('')
    if (!host) {
      setInitializationError(yamlMonacoInitializationError(new Error('The Monaco host element was unavailable.')))
      return () => {
        active = false
      }
    }

    const handleWorkerFailure = (): void => {
      if (!active) return
      disposeYamlMonacoSession(editorRef, modelRef, changeListenerRef)
      disposeYamlLanguage()
      setReady(false)
      setInitializationError(yamlMonacoInitializationError(new Error('A required Monaco worker stopped.')))
    }

    try {
      configureYamlMonacoWorkers(handleWorkerFailure)
      configureYamlLanguage()
      defineYamlMonacoTheme()
      const uri = monaco.Uri.parse(yamlMonacoModelUri(scopeKey))
      monaco.editor.getModel(uri)?.dispose()
      const model = monaco.editor.createModel(value, 'yaml', uri)
      const editor = monaco.editor.create(host, {
        ...yamlMonacoEditorOptions(readOnly),
        ariaLabel: 'Resource YAML',
        fontFamily: "'DM Mono', 'SFMono-Regular', Consolas, monospace",
        fontSize: 12,
        lineHeight: 19,
        lineNumbers: 'on',
        minimap: { enabled: false },
        model,
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
        model.dispose()
        return () => undefined
      }

      editorRef.current = editor
      modelRef.current = model
      changeListenerRef.current = model.onDidChangeContent(() => {
        if (!syncingRef.current) onChangeRef.current(model.getValue())
      })
      setReady(true)
    } catch (error) {
      disposeYamlMonacoSession(editorRef, modelRef, changeListenerRef)
      if (active) setInitializationError(yamlMonacoInitializationError(error))
    }

    return () => {
      active = false
      if (monacoWorkerFailureHandler === handleWorkerFailure) monacoWorkerFailureHandler = undefined
      disposeYamlMonacoSession(editorRef, modelRef, changeListenerRef)
      disposeYamlLanguage()
    }
  }, [scopeKey])

  useLayoutEffect(() => {
    const editor = editorRef.current
    const model = modelRef.current
    const change = model ? yamlEditorDocumentChange(model.getValue(), value) : undefined
    if (!editor || !model || !change) return

    const selections = editor.getSelections()?.map((selection) => ({
      anchor: model.getOffsetAt({ lineNumber: selection.selectionStartLineNumber, column: selection.selectionStartColumn }),
      active: model.getOffsetAt({ lineNumber: selection.positionLineNumber, column: selection.positionColumn }),
    })) || []
    syncingRef.current = true
    try {
      model.applyEdits([{ range: model.getFullModelRange(), text: change.insert }])
      const length = model.getValueLength()
      editor.setSelections(selections.map((selection) => {
        const anchor = model.getPositionAt(Math.min(selection.anchor, length))
        const active = model.getPositionAt(Math.min(selection.active, length))
        return new monaco.Selection(anchor.lineNumber, anchor.column, active.lineNumber, active.column)
      }))
    } finally {
      syncingRef.current = false
    }
  }, [value])

  useEffect(() => {
    editorRef.current?.updateOptions({ readOnly, domReadOnly: readOnly })
  }, [readOnly])

  return <div className="yaml-monaco-editor">
    <div ref={hostRef} className={`yaml-monaco-host${initializationError ? ' yaml-monaco-host-blocked' : ''}`} />
    {initializationError
      ? <div className="yaml-editor-error" role="alert"><AlertTriangle size={16} /><div><strong>Monaco YAML editor unavailable</strong><span>{initializationError}</span></div></div>
      : !ready && <div className="yaml-editor-notice" role="status"><LoaderCircle size={14} className="spin" /><span>Starting the local Monaco YAML editor and required workers.</span></div>}
  </div>
}
