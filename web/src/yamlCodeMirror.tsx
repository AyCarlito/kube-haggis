import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { bracketMatching, codeFolding, foldGutter, foldKeymap, foldNodeProp, foldService, foldedRanges, HighlightStyle, indentOnInput, syntaxHighlighting, unfoldAll, unfoldEffect } from '@codemirror/language'
import { Compartment, EditorState, StateEffect, StateField, Transaction, type Extension, type TransactionSpec } from '@codemirror/state'
import { EditorView, drawSelection, highlightActiveLine, keymap, lineNumbers, type ViewUpdate } from '@codemirror/view'
import { yamlLanguage } from '@codemirror/lang-yaml'
import { tags } from '@lezer/highlight'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { AlertTriangle } from 'lucide-react'
import {
  buildYamlFoldModel,
  findYamlFoldRangeByOffsets,
  findYamlFoldRangeForLine,
  type YamlFoldModel,
  type YamlFoldPhysicalRange,
} from './yamlFolding'

/** Describes the accessible state exposed by one CodeMirror fold marker. */
export interface YamlFoldMarkerAccessibility {
  ariaExpanded: boolean
  ariaLabel: string
  title: string
}

/** Describes the safe text used by a physical fold placeholder. */
export interface YamlFoldPlaceholderData {
  label: string
  lineCount: number
}

/** Describes one complete replacement used to synchronize an external YAML draft. */
export interface YamlEditorDocumentChange {
  from: number
  to: number
  insert: string
}

/** Defines the raw-draft inputs and resource scope for the CodeMirror YAML editor. */
export interface YamlCodeMirrorEditorProps {
  value: string
  readOnly: boolean
  scopeKey: string
  onChange: (value: string) => void
}

/** Describes a bounded source interval accepted by a CodeMirror fold service. */
interface YamlCodeMirrorDocRange {
  from: number
  to: number
}

/** Builds keyboard and screen-reader metadata for a fold gutter button. */
export function yamlFoldMarkerAccessibility(open: boolean): YamlFoldMarkerAccessibility {
  const action = open ? 'Collapse' : 'Expand'
  return {
    ariaExpanded: open,
    ariaLabel: `${action} YAML block`,
    title: `${action} YAML block`,
  }
}

/** Builds a safe placeholder description without reading folded YAML values. */
export function yamlFoldPlaceholderData(range: YamlFoldPhysicalRange | undefined, lineCount: number): YamlFoldPlaceholderData {
  return {
    label: range?.label || 'nested YAML',
    lineCount: Math.max(1, range?.lineCount || lineCount),
  }
}

/** Builds the non-history replacement used when an external draft changes the editor document. */
export function yamlEditorDocumentChange(current: string, next: string): YamlEditorDocumentChange | undefined {
  if (current === next) return undefined
  return { from: 0, to: current.length, insert: next }
}

/** Stores the latest bounded YAML fold model inside the stable CodeMirror state. */
const yamlFoldModelEffect = StateEffect.define<YamlFoldModel | undefined>()

/** Applies a newly parsed YAML fold model without changing the editor document. */
export function yamlFoldModelEffectOf(model: YamlFoldModel | undefined): StateEffect<YamlFoldModel | undefined> {
  return yamlFoldModelEffect.of(model)
}

/** Invalidates stale structural folds as soon as the editor document changes. */
const yamlFoldModelField = StateField.define<YamlFoldModel | undefined>({
  create: () => undefined,
  update(value, transaction) {
    if (transaction.docChanged) return undefined
    for (const effect of transaction.effects) {
      if (effect.is(yamlFoldModelEffect)) return effect.value
    }
    return value
  },
})

/** Rebuilds document transactions with mapped unfold effects instead of dispatching reentrantly. */
function clearYamlFoldsOnDocumentChange(transaction: Transaction): TransactionSpec {
  const effects = [...transaction.effects]
  if (transaction.docChanged) {
    foldedRanges(transaction.startState).between(0, transaction.startState.doc.length, (from, to) => {
      const mappedFrom = transaction.changes.mapPos(from, 1)
      const mappedTo = transaction.changes.mapPos(to, -1)
      if (mappedTo >= mappedFrom) effects.push(unfoldEffect.of({ from: mappedFrom, to: mappedTo }))
    })
  }

  const addToHistory = transaction.annotation(Transaction.addToHistory)
  const userEvent = transaction.annotation(Transaction.userEvent)
  const remote = transaction.annotation(Transaction.remote)
  return {
    changes: transaction.changes,
    selection: transaction.selection,
    effects,
    annotations: [
      ...(addToHistory === undefined ? [] : [Transaction.addToHistory.of(addToHistory)]),
      ...(userEvent === undefined ? [] : [Transaction.userEvent.of(userEvent)]),
      ...(remote === undefined ? [] : [Transaction.remote.of(remote)]),
    ],
    scrollIntoView: transaction.scrollIntoView,
    filter: false,
  }
}

/** Prevents CodeMirror's partial YAML syntax tree from exposing unsafe fallback folds. */
const safeYamlLanguage = yamlLanguage.configure({
  props: [foldNodeProp.add(() => () => null)],
})

/** Keeps CRLF characters in the document so CodeMirror round-trips the raw draft exactly. */
const yamlRawLineSeparator = EditorState.lineSeparator.of('\n')

/** Returns only a bounded fold range approved by the complete local YAML model. */
function safeYamlFoldService(state: EditorState, lineStart: number, _lineEnd: number): YamlCodeMirrorDocRange | null {
  const model = state.field(yamlFoldModelField, false)
  if (!model) return null
  const source = state.doc.toString()
  const line = state.doc.lineAt(lineStart).number - 1
  const range = findYamlFoldRangeForLine(model, source, line)
  return range ? { from: range.from, to: range.to } : null
}

/** Tells the fold gutter to re-check markers when a bounded model arrives. */
function yamlFoldModelChanged(update: ViewUpdate): boolean {
  return update.transactions.some((transaction) => transaction.effects.some((effect) => effect.is(yamlFoldModelEffect)))
}

/** Creates a keyboard-accessible fold gutter marker using KubeHaggis focus treatment. */
function yamlFoldMarkerDOM(open: boolean): HTMLElement {
  const marker = document.createElement('button')
  const accessibility = yamlFoldMarkerAccessibility(open)
  marker.type = 'button'
  marker.className = 'yaml-fold-marker'
  marker.textContent = open ? 'v' : '>'
  marker.setAttribute('aria-expanded', String(accessibility.ariaExpanded))
  marker.setAttribute('aria-label', accessibility.ariaLabel)
  marker.title = accessibility.title
  return marker
}

/** Prepares safe fold placeholder metadata from source offsets and the active model. */
function yamlFoldPreparePlaceholder(state: EditorState, range: YamlCodeMirrorDocRange): YamlFoldPlaceholderData {
  const source = state.doc.toString()
  const model = state.field(yamlFoldModelField, false)
  const physicalRange = model ? findYamlFoldRangeByOffsets(model, source, range.from, range.to) : undefined
  const lineCount = state.doc.sliceString(range.from, range.to).split(/\r?\n/).length
  return yamlFoldPlaceholderData(physicalRange, lineCount)
}

/** Creates a keyboard-accessible physical fold widget without exposing folded values. */
function yamlFoldPlaceholderDOM(_view: EditorView, onclick: (event: Event) => void, prepared: unknown): HTMLElement {
  const data = prepared as YamlFoldPlaceholderData
  const placeholder = document.createElement('button')
  placeholder.type = 'button'
  placeholder.className = 'yaml-fold-placeholder'
  placeholder.textContent = `... ${data.lineCount} folded ${data.lineCount === 1 ? 'line' : 'lines'}`
  placeholder.setAttribute('aria-label', `Expand folded ${data.label}; ${data.lineCount} hidden ${data.lineCount === 1 ? 'line' : 'lines'}`)
  placeholder.title = `Expand ${data.label}`
  placeholder.addEventListener('click', onclick)
  return placeholder
}

/** Defines the KubeHaggis YAML syntax palette used by the CodeMirror highlighter. */
const yamlHighlighting = HighlightStyle.define([
  { tag: tags.comment, color: '#697265' },
  { tag: tags.propertyName, color: '#f09a62' },
  { tag: tags.string, color: '#a8bd83' },
  { tag: [tags.number, tags.bool, tags.null], color: '#e6b66f' },
  { tag: tags.atom, color: '#87aeb6' },
  { tag: tags.typeName, color: '#d7d6c8' },
  { tag: tags.punctuation, color: '#8e9687' },
  { tag: tags.operator, color: '#d77b4d' },
])

/** Defines editor-level colors and spacing while leaving responsive sizing to styles.css. */
const yamlCodeMirrorTheme = EditorView.theme({
  '&': {
    color: 'var(--paper)',
    backgroundColor: 'transparent',
  },
  '.cm-scroller': {
    overflow: 'auto',
    fontFamily: 'var(--mono)',
    lineHeight: '1.55',
  },
  '.cm-content': {
    minHeight: '100%',
    padding: '14px 15px 18px 10px',
    caretColor: 'var(--copper-bright)',
  },
  '.cm-line': {
    padding: '0',
  },
  '.cm-gutters': {
    borderRight: '1px solid rgba(226, 224, 204, .07)',
    backgroundColor: 'rgba(11, 14, 11, .94)',
    color: 'var(--muted-dark)',
  },
  '.cm-lineNumbers .cm-gutterElement': {
    minWidth: '32px',
    padding: '0 7px 0 9px',
    textAlign: 'right',
  },
  '.cm-foldGutter': {
    minWidth: '27px',
  },
  '.cm-activeLine': {
    backgroundColor: 'rgba(215, 123, 77, .045)',
  },
  '.cm-activeLineGutter': {
    backgroundColor: 'rgba(215, 123, 77, .09)',
    color: 'var(--copper-bright)',
  },
  '&.cm-focused .cm-cursor': {
    borderLeftColor: 'var(--copper-bright)',
  },
  '&.cm-focused .cm-selectionBackground, ::selection': {
    backgroundColor: 'rgba(215, 123, 77, .24)',
  },
})

/** Creates the stable CodeMirror extension set used for every YAML resource draft. */
export function createYamlCodeMirrorExtensions(): Extension {
  return [
    yamlRawLineSeparator,
    safeYamlLanguage.extension,
    yamlFoldModelField,
    EditorState.transactionFilter.of(clearYamlFoldsOnDocumentChange),
    foldService.of(safeYamlFoldService),
    lineNumbers(),
    foldGutter({
      markerDOM: yamlFoldMarkerDOM,
      foldingChanged: yamlFoldModelChanged,
    }),
    codeFolding({
      placeholderDOM: yamlFoldPlaceholderDOM,
      preparePlaceholder: yamlFoldPreparePlaceholder,
    }),
    history(),
    keymap.of([...defaultKeymap, ...historyKeymap, ...foldKeymap, indentWithTab]),
    drawSelection(),
    highlightActiveLine(),
    bracketMatching(),
    indentOnInput(),
    syntaxHighlighting(yamlHighlighting),
    EditorView.contentAttributes.of({ 'aria-label': 'Resource YAML', spellcheck: 'false' }),
    yamlCodeMirrorTheme,
  ]
}

/** Dispatches CodeMirror transactions while preserving the parent draft as the source of truth. */
function dispatchYamlTransactions(
  transactions: readonly Transaction[],
  view: EditorView,
  syncing: { current: boolean },
  onChange: { current: (value: string) => void },
): void {
  const documentChanged = transactions.some((transaction) => transaction.docChanged)
  view.update(transactions)
  if (documentChanged && !syncing.current) onChange.current(view.state.doc.toString())
}

/** Renders a stable CodeMirror YAML editor with a raw textarea fallback for initialization failures. */
export function YamlCodeMirrorEditor({ value, readOnly, scopeKey, onChange }: YamlCodeMirrorEditorProps) {
  const hostRef = useRef<HTMLDivElement | null>(null)
  const viewRef = useRef<EditorView | null>(null)
  const editableCompartmentRef = useRef(new Compartment())
  const syncingRef = useRef(false)
  const onChangeRef = useRef(onChange)
  const parseEpochRef = useRef(0)
  const [model, setModel] = useState<YamlFoldModel | undefined>()
  const [initializationError, setInitializationError] = useState(false)

  useEffect(() => {
    onChangeRef.current = onChange
  }, [onChange])

  useLayoutEffect(() => {
    const host = hostRef.current
    if (!host || viewRef.current) return
    try {
      const editable = editableCompartmentRef.current
      const view = new EditorView({
        parent: host,
        state: EditorState.create({
          doc: value,
          extensions: [
            createYamlCodeMirrorExtensions(),
            editable.of([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]),
          ],
        }),
        dispatchTransactions: (transactions, currentView) => dispatchYamlTransactions(transactions, currentView, syncingRef, onChangeRef),
      })
      viewRef.current = view
    } catch {
      setInitializationError(true)
    }

    return () => {
      parseEpochRef.current += 1
      viewRef.current?.destroy()
      viewRef.current = null
    }
  }, [])

  useLayoutEffect(() => {
    const view = viewRef.current
    const replacement = view ? yamlEditorDocumentChange(view.state.doc.toString(), value) : undefined
    if (!view || !replacement) return
    syncingRef.current = true
    unfoldAll(view)
    view.dispatch({
      changes: replacement,
      annotations: Transaction.addToHistory.of(false),
    })
    syncingRef.current = false
  }, [value])

  useEffect(() => {
    const view = viewRef.current
    if (view) unfoldAll(view)
    setModel(undefined)
  }, [scopeKey])

  useEffect(() => {
    const parseEpoch = ++parseEpochRef.current
    let active = true
    const timeoutID = window.setTimeout(() => {
      if (!active || parseEpoch !== parseEpochRef.current) return
      const nextModel = buildYamlFoldModel(value)
      if (!active || parseEpoch !== parseEpochRef.current) return
      const view = viewRef.current
      if (view && view.state.doc.toString() !== value) return
      setModel(nextModel)
      if (!view) return
      view.dispatch({ effects: yamlFoldModelEffectOf(nextModel) })
    }, 0)
    return () => {
      active = false
      window.clearTimeout(timeoutID)
    }
  }, [scopeKey, value])

  useEffect(() => {
    const view = viewRef.current
    if (!view) return
    view.dispatch({ effects: editableCompartmentRef.current.reconfigure([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]) })
  }, [readOnly])

  const currentModel = model?.source === value ? model : undefined
  const modelNotice = currentModel?.status === 'invalid'
    ? 'Folding is paused while this YAML is incomplete or invalid; the raw draft remains editable.'
    : currentModel?.status === 'bounded'
      ? 'Folding is paused for this large document; the raw draft remains available.'
      : ''
  const notice = initializationError
    ? 'CodeMirror could not initialize; the raw YAML editor remains available.'
    : modelNotice

  return <div className="yaml-code-editor">
    {initializationError ? <textarea
      className="yaml-editor-fallback"
      value={value}
      onChange={(event) => onChange(event.target.value)}
      readOnly={readOnly}
      spellCheck={false}
      wrap="off"
      aria-label="Resource YAML"
    /> : <div ref={hostRef} className="yaml-codemirror-host" />}
    {notice && <div className="yaml-editor-notice" role="status"><AlertTriangle size={14} /> <span>{notice}</span></div>}
  </div>
}
