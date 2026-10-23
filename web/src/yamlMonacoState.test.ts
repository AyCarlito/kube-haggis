import {
  YAML_MONACO_WORKER_LABELS,
  yamlEditorDocumentChange,
  yamlMonacoEditorOptions,
  yamlMonacoInitializationError,
  yamlMonacoModelUri,
  yamlMonacoWorkerLabelSupported,
} from './yamlMonacoState'

/** Fails a focused Monaco YAML editor contract assertion with a readable message. */
function assertYamlMonaco(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises native indentation folding and read-only options without constructing a DOM editor. */
function testNativeFoldingOptions(): void {
  const editable = yamlMonacoEditorOptions(false)
  const readOnly = yamlMonacoEditorOptions(true)
  assertYamlMonaco(editable.folding && editable.foldingHighlight, 'Monaco native folding was not enabled')
  assertYamlMonaco(editable.foldingStrategy === 'indentation' && editable.showFoldingControls === 'always', 'Monaco did not use accessible indentation folding controls')
  assertYamlMonaco(editable.largeFileOptimizations && editable.foldingMaximumRegions === 512 && editable.maxTokenizationLineLength === 20_000 && editable.stopRenderingLineAfter === 10_000, 'Monaco bounded rendering safeguards were not enabled')
  assertYamlMonaco(!editable.readOnly && !editable.domReadOnly && readOnly.readOnly && readOnly.domReadOnly, 'Monaco read-only options did not follow the resource write mode')
}

/** Exercises exact full-document synchronization while retaining raw line endings and values. */
function testCompleteDraftSynchronization(): void {
  const current = 'kind: Secret\r\ndata:\r\n  token: old-secret\r\n'
  const next = 'kind: Secret\r\ndata:\r\n  token: new-secret\r\n'
  const change = yamlEditorDocumentChange(current, next)
  assertYamlMonaco(change?.from === 0 && change.to === current.length && change.insert === next, 'external synchronization did not replace the complete raw YAML document')
  assertYamlMonaco(yamlEditorDocumentChange(current, current) === undefined, 'a no-op parent update would unnecessarily replace the Monaco model')
}

/** Exercises resource-scope URI isolation and fresh model identity for navigation. */
function testResourceScopeIsolation(): void {
  const first = yamlMonacoModelUri('ctx|apps|v1|deployments|team-a|first')
  const second = yamlMonacoModelUri('ctx|apps|v1|deployments|team-a|second')
  assertYamlMonaco(first !== second && first.startsWith('inmemory://kube-haggis/yaml/'), 'resource scopes did not receive isolated browser-local Monaco model URIs')
  assertYamlMonaco(!first.includes('|'), 'resource scope separators were not encoded in the model URI')
}

/** Exercises the required worker labels and the explicit initialization failure contract. */
function testWorkerAndFailureContracts(): void {
  assertYamlMonaco(YAML_MONACO_WORKER_LABELS.join(',') === 'editorWorkerService,yaml', 'the required Monaco worker labels changed')
  assertYamlMonaco(yamlMonacoWorkerLabelSupported('yaml') && yamlMonacoWorkerLabelSupported('editorWorkerService'), 'required Monaco worker labels were not recognized')
  assertYamlMonaco(!yamlMonacoWorkerLabelSupported('json'), 'an unconfigured worker label was accepted')
  const message = yamlMonacoInitializationError(new Error('secret-value-must-not-appear'))
  assertYamlMonaco(message.includes('Monaco YAML editor initialization failed') && message.includes('no alternate editor'), 'initialization failure did not produce an explicit blocking message')
  assertYamlMonaco(!message.includes('secret-value-must-not-appear'), 'initialization errors exposed arbitrary error detail')
}

/** Runs deterministic Monaco model/state coverage; browser worker and DOM lifecycle behavior remains runtime-only. */
export function runYamlMonacoStateTests(): void {
  testNativeFoldingOptions()
  testCompleteDraftSynchronization()
  testResourceScopeIsolation()
  testWorkerAndFailureContracts()
}

runYamlMonacoStateTests()
