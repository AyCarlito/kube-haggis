/** Describes one complete replacement used to synchronize an external YAML draft. */
export interface YamlEditorDocumentChange {
  from: number
  to: number
  insert: string
}

/** Describes the Monaco options that make native YAML folding the editor contract. */
export interface YamlMonacoEditorOptions {
  automaticLayout: boolean
  contextmenu: boolean
  folding: boolean
  foldingHighlight: boolean
  foldingStrategy: 'indentation'
  showFoldingControls: 'always'
  glyphMargin: boolean
  largeFileOptimizations: boolean
  foldingMaximumRegions: number
  maxTokenizationLineLength: number
  stopRenderingLineAfter: number
  find: {
    seedSearchStringFromSelection: 'always' | 'never' | 'selection'
  }
  readOnly: boolean
  domReadOnly: boolean
}

/** Identifies the worker labels required by the standalone Monaco YAML editor. */
export type YamlMonacoWorkerLabel = 'editorWorkerService' | 'yaml'

/** Lists the worker labels that must be available before the editor can start. */
export const YAML_MONACO_WORKER_LABELS: readonly YamlMonacoWorkerLabel[] = ['editorWorkerService', 'yaml']

/** Builds a full-document replacement while leaving no-op parent updates untouched. */
export function yamlEditorDocumentChange(current: string, next: string): YamlEditorDocumentChange | undefined {
  if (current === next) return undefined
  return { from: 0, to: current.length, insert: next }
}

/** Builds a stable browser-local model URI that isolates one resource scope. */
export function yamlMonacoModelUri(scopeKey: string): string {
  const encodedScope = encodeURIComponent(scopeKey || 'unscoped')
  return `inmemory://kube-haggis/yaml/${encodedScope}.yaml`
}

/** Builds the browser-local URI for the accepted cluster snapshot model. */
export function yamlMonacoOriginalModelUri(scopeKey: string): string {
  const encodedScope = encodeURIComponent(scopeKey || 'unscoped')
  return `inmemory://kube-haggis/yaml/${encodedScope}.baseline.yaml`
}

/** Builds a disposable browser-local URI for one read-only hunk peek side. */
export function yamlMonacoHunkModelUri(scopeKey: string, hunkID: string, side: 'original' | 'modified'): string {
  const encodedScope = encodeURIComponent(`${scopeKey || 'unscoped'}|peek|${hunkID}|${side}`)
  return `inmemory://kube-haggis/yaml/${encodedScope}.yaml`
}

/** Returns the native Monaco editor options shared by every YAML resource scope. */
export function yamlMonacoEditorOptions(readOnly: boolean): YamlMonacoEditorOptions {
  return {
    automaticLayout: true,
    contextmenu: true,
    folding: true,
    foldingHighlight: true,
    foldingStrategy: 'indentation',
    showFoldingControls: 'always',
    glyphMargin: true,
    largeFileOptimizations: true,
    foldingMaximumRegions: 512,
    maxTokenizationLineLength: 20_000,
    stopRenderingLineAfter: 10_000,
    find: { seedSearchStringFromSelection: 'always' },
    readOnly,
    domReadOnly: readOnly,
  }
}

/** Identifies worker labels supported by the required local worker environment. */
export function yamlMonacoWorkerLabelSupported(label: string): label is YamlMonacoWorkerLabel {
  return (YAML_MONACO_WORKER_LABELS as readonly string[]).includes(label)
}

/** Returns a safe blocking message for Monaco or worker initialization failures. */
export function yamlMonacoInitializationError(_error: unknown): string {
  return 'Monaco YAML editor initialization failed because a required local worker could not start. Reload the page to retry; no alternate editor is available.'
}

/** Returns a safe blocking message when Monaco cannot initialize the native comparison. */
export function yamlMonacoComparisonInitializationError(_error: unknown): string {
  return 'Native Monaco comparison could not initialize. The editable Monaco draft remains available; reload the page to retry. No alternate diff surface is available.'
}

/** Returns a safe blocking message when a native Monaco hunk peek cannot initialize. */
export function yamlMonacoHunkPeekInitializationError(_error: unknown): string {
  return 'Native Monaco hunk peek is unavailable for this change. The editable Monaco draft remains unchanged; reload the page to retry.'
}
