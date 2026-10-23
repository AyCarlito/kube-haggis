import { foldEffect, foldable, foldedRanges } from '@codemirror/language'
import { EditorState, Transaction } from '@codemirror/state'
import { undoDepth } from '@codemirror/commands'
import { buildYamlFoldModel, mapYamlFoldNodeToSourceRange, type YamlFoldModel } from './yamlFolding'
import {
  createYamlCodeMirrorExtensions,
  yamlEditorDocumentChange,
  yamlFoldMarkerAccessibility,
  yamlFoldModelEffectOf,
  yamlFoldPlaceholderData,
} from './yamlCodeMirror'

/** Fails a focused CodeMirror YAML editor assertion with a readable message. */
function assertYamlEditor(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Returns a state carrying one bounded YAML fold model for deterministic editor tests. */
function stateWithModel(source: string, model: YamlFoldModel = buildYamlFoldModel(source)): EditorState {
  const state = EditorState.create({ doc: source, extensions: createYamlCodeMirrorExtensions() })
  return state.update({ effects: yamlFoldModelEffectOf(model) }).state
}

/** Collects physical fold decorations without depending on a browser DOM. */
function physicalFoldRanges(state: EditorState): Array<{ from: number; to: number }> {
  const ranges: Array<{ from: number; to: number }> = []
  foldedRanges(state).between(0, state.doc.length, (from, to) => { ranges.push({ from, to }) })
  return ranges
}

/** Exercises nested mapping and sequence line lookup through the real CodeMirror fold service. */
function testNestedPhysicalFolds(): void {
  const source = 'spec:\n  containers:\n    - name: app\n      env:\n        - name: MODE\n          value: safe\n'
  const model = buildYamlFoldModel(source)
  const state = stateWithModel(source, model)
  const spec = model.nodes.find((node) => node.label === 'spec')
  const env = model.nodes.find((node) => node.label === 'env')
  assertYamlEditor(spec !== undefined && env !== undefined, 'nested fold test fixture did not contain expected nodes')
  const specLine = state.doc.line(spec!.line + 1)
  const envLine = state.doc.line(env!.line + 1)
  const specRange = foldable(state, specLine.from, specLine.to)
  const envRange = foldable(state, envLine.from, envLine.to)
  assertYamlEditor(specRange?.from === mapYamlFoldNodeToSourceRange(source, spec!)?.from, 'CodeMirror mapping fold did not use the physical child offset')
  assertYamlEditor(envRange?.to === mapYamlFoldNodeToSourceRange(source, env!)?.to, 'CodeMirror nested fold did not use the physical collection end')
}

/** Exercises physical collapse while keeping document text, selection, and history untouched. */
function testPhysicalCollapsePreservesDraftState(): void {
  const source = 'spec:\n  nested:\n    enabled: true\n'
  const model = buildYamlFoldModel(source)
  const state = stateWithModel(source, model)
  const spec = model.nodes.find((node) => node.label === 'spec')
  assertYamlEditor(spec !== undefined, 'physical collapse fixture did not contain a mapping fold')
  const line = state.doc.line(spec!.line + 1)
  const range = foldable(state, line.from, line.to)
  assertYamlEditor(range !== null, 'validated mapping did not become foldable in CodeMirror')
  const beforeSelection = state.selection
  const beforeHistoryDepth = undoDepth(state)
  const folded = state.update({ effects: foldEffect.of(range!) }).state
  const ranges = physicalFoldRanges(folded)
  assertYamlEditor(ranges.length === 1 && ranges[0].from === range!.from && ranges[0].to === range!.to, 'CodeMirror did not store one physical fold decoration')
  assertYamlEditor(folded.doc.toString() === source, 'physical folding changed the complete YAML document')
  assertYamlEditor(folded.selection.eq(beforeSelection), 'physical folding changed the editor selection')
  assertYamlEditor(undoDepth(folded) === beforeHistoryDepth, 'physical folding added an entry to the YAML undo history')
  const changed = folded.update({ changes: { from: source.length, insert: '# note\n' } }).state
  assertYamlEditor(physicalFoldRanges(changed).length === 0, 'document changes retained a stale physical fold')
  const replacement = yamlEditorDocumentChange(changed.doc.toString(), source)
  assertYamlEditor(replacement?.from === 0 && replacement?.to === changed.doc.length && replacement.insert === source, 'external draft synchronization did not replace the complete document')
  const synchronized = changed.update({ changes: replacement!, annotations: Transaction.addToHistory.of(false) }).state
  assertYamlEditor(synchronized.doc.toString() === source, 'external draft synchronization lost the complete source')
  const cleanExternalUpdate = state.update({ changes: { from: 0, to: source.length, insert: source + '# external\n' }, annotations: Transaction.addToHistory.of(false) }).state
  assertYamlEditor(undoDepth(cleanExternalUpdate) === 0, 'external draft synchronization created an unexpected undo entry')
}

/** Exercises fresh editor-state semantics used when Resource Detail changes scope keys. */
function testResourceScopeStartsFreshEditorState(): void {
  const firstSource = 'spec:\n  nested:\n    enabled: true\n'
  const firstModel = buildYamlFoldModel(firstSource)
  const firstState = stateWithModel(firstSource, firstModel)
  const firstNode = firstModel.nodes.find((node) => node.label === 'spec')
  assertYamlEditor(firstNode !== undefined, 'scope isolation fixture did not contain the first fold')
  const firstRange = foldable(firstState, firstState.doc.line(firstNode!.line + 1).from, firstState.doc.line(firstNode!.line + 1).to)
  const editedFirstState = firstState
    .update({ effects: foldEffect.of(firstRange!) })
    .state
    .update({ selection: { anchor: 5, head: 17 } })
    .state
    .update({ changes: { from: firstSource.length, insert: '# first edit\n' } })
    .state
  assertYamlEditor(undoDepth(editedFirstState) > 0 && editedFirstState.selection.main.anchor !== 0, 'first resource fixture did not accumulate isolated editor state')

  const secondSource = 'metadata:\n  name: second\n'
  const secondState = stateWithModel(secondSource)
  assertYamlEditor(secondState.doc.toString() === secondSource, 'scope change did not load the second resource document')
  assertYamlEditor(secondState.selection.main.anchor === 0 && secondState.selection.main.head === 0, 'scope change carried the prior resource selection')
  assertYamlEditor(undoDepth(secondState) === 0, 'scope change carried the prior resource undo history')
  assertYamlEditor(physicalFoldRanges(secondState).length === 0, 'scope change carried the prior resource fold state')
}

/** Exercises exact CRLF round-tripping while retaining physical fold offsets. */
function testRawLineEndingPreservation(): void {
  const source = 'spec:\r\n  nested:\r\n    enabled: true\r\n'
  const model = buildYamlFoldModel(source)
  const state = stateWithModel(source, model)
  const spec = model.nodes.find((node) => node.label === 'spec')
  assertYamlEditor(state.doc.toString() === source, 'CodeMirror normalized the raw CRLF YAML draft')
  assertYamlEditor(spec !== undefined, 'CRLF fixture did not contain a physical fold')
  const line = state.doc.line(spec!.line + 1)
  const range = foldable(state, line.from, line.to)
  assertYamlEditor(range?.from === mapYamlFoldNodeToSourceRange(source, spec!)?.from, 'CRLF fold offsets did not remain aligned with the raw source')
  const nextSource = `${source}# external\r\n`
  const replacement = yamlEditorDocumentChange(source, nextSource)
  const synchronized = state.update({ changes: replacement!, annotations: Transaction.addToHistory.of(false) }).state
  assertYamlEditor(synchronized.doc.toString() === nextSource, 'external CRLF draft synchronization normalized the raw YAML source')
}

/** Exercises invalid and bounded drafts with the YAML parser fallback disabled. */
function testUnsafeDraftsHaveNoFoldService(): void {
  const invalidSource = 'spec:\n  containers:\n    - name: app\n      env: [\n'
  const invalidState = stateWithModel(invalidSource)
  const invalidLine = invalidState.doc.line(1)
  assertYamlEditor(foldable(invalidState, invalidLine.from, invalidLine.to) === null, 'invalid YAML exposed a partial parser fold')

  const boundedSource = 'x'.repeat(160_001)
  const boundedState = stateWithModel(boundedSource)
  const boundedLine = boundedState.doc.line(1)
  assertYamlEditor(foldable(boundedState, boundedLine.from, boundedLine.to) === null, 'bounded YAML exposed an unsafe parser fold')
}

/** Exercises keyboard-visible marker state and value-free fold placeholder labels. */
function testFoldAccessibilityState(): void {
  const open = yamlFoldMarkerAccessibility(true)
  const closed = yamlFoldMarkerAccessibility(false)
  assertYamlEditor(open.ariaExpanded && !closed.ariaExpanded, 'fold marker expanded state did not invert')
  assertYamlEditor(open.ariaLabel === 'Collapse YAML block' && closed.ariaLabel === 'Expand YAML block', 'fold marker labels were not keyboard and screen-reader friendly')
  const placeholder = yamlFoldPlaceholderData({ nodeId: 'm0', kind: 'mapping', label: 'data', from: 1, to: 12, lineCount: 3 }, 99)
  assertYamlEditor(placeholder.label === 'data' && placeholder.lineCount === 3, 'fold placeholder metadata did not retain safe structural context')
}

/** Runs deterministic CodeMirror state coverage for U17 physical folding behavior. */
export function runYamlCodeMirrorTests(): void {
  testNestedPhysicalFolds()
  testPhysicalCollapsePreservesDraftState()
  testResourceScopeStartsFreshEditorState()
  testRawLineEndingPreservation()
  testUnsafeDraftsHaveNoFoldService()
  testFoldAccessibilityState()
}

runYamlCodeMirrorTests()
