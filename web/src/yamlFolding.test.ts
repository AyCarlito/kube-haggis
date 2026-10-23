import {
  buildYamlFoldModel,
  buildYamlFoldPresentation,
  toggleYamlFold,
  yamlFoldControl,
  YAML_FOLDING_MAX_CHARACTERS,
  type YamlFoldNode,
} from './yamlFolding'

/** Fails a focused YAML folding model assertion with a readable message. */
function assertYamlFolding(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Finds one structural node by its kind and display-safe label. */
function findYamlFoldNode(nodes: YamlFoldNode[], kind: YamlFoldNode['kind'], label: string): YamlFoldNode {
  const node = nodes.find((candidate) => candidate.kind === kind && candidate.label === label)
  if (!node) throw new Error(`Missing YAML fold node ${kind}/${label}`)
  return node
}

/** Exercises nested mappings, sequences, scalar exclusions, and multiline sequence items. */
function testNestedYamlStructures(): void {
  const source = [
    'apiVersion: v1',
    'spec:',
    '  template:',
    '    metadata:',
    '      labels:',
    '        app: demo',
    '    containers:',
    '      - name: app',
    '        env:',
    '          - name: MODE',
    '            value: safe',
    '        ports:',
    '          - containerPort: 8080',
  ].join('\n') + '\n'
  const model = buildYamlFoldModel(source)
  assertYamlFolding(model.status === 'ready', 'valid nested YAML did not produce a ready folding model')

  const spec = findYamlFoldNode(model.nodes, 'mapping', 'spec')
  const template = findYamlFoldNode(model.nodes, 'mapping', 'template')
  const containers = findYamlFoldNode(model.nodes, 'mapping', 'containers')
  const containerItem = findYamlFoldNode(model.nodes, 'sequence-item', 'item 1')
  const env = findYamlFoldNode(model.nodes, 'mapping', 'env')
  const envItem = model.nodes.find((node) => node.kind === 'sequence-item' && node.parentId === env.id)
  const ports = findYamlFoldNode(model.nodes, 'mapping', 'ports')

  assertYamlFolding(spec.endLine > spec.line && template.parentId === spec.id, 'nested mapping parentage was not retained')
  assertYamlFolding(containers.parentId === template.id, 'mapping-to-sequence nesting was not retained')
  assertYamlFolding(containerItem.parentId === containers.id, 'sequence item parentage was not retained')
  assertYamlFolding(envItem !== undefined && env.parentId === containerItem.id && envItem.parentId === env.id, 'mapping-inside-sequence nesting was not retained')
  assertYamlFolding(ports.parentId === containerItem.id, 'second nested sequence was not retained')
  assertYamlFolding(!model.nodes.some((node) => node.kind === 'mapping' && node.label === 'apiVersion'), 'scalar mapping entry exposed a fold control')
  assertYamlFolding(!model.nodes.some((node) => node.kind === 'mapping' && node.label === 'name'), 'scalar-only sequence mapping entries exposed a fold control')
  assertYamlFolding(model.nodes.filter((node) => node.kind === 'sequence-item').length === 2, 'nested expandable sequence items were not identified independently')
}

/** Exercises independent nested fold state and presentation range suppression. */
function testIndependentNestedFoldState(): void {
  const source = 'spec:\n  containers:\n    - name: app\n      env:\n        - name: MODE\n          value: safe\n'
  const model = buildYamlFoldModel(source)
  const spec = findYamlFoldNode(model.nodes, 'mapping', 'spec')
  const env = findYamlFoldNode(model.nodes, 'mapping', 'env')

  const specCollapsed = toggleYamlFold(new Set(), spec.id)
  const specPresentation = buildYamlFoldPresentation(model, specCollapsed)
  assertYamlFolding(specPresentation.ranges.length === 1 && specPresentation.ranges[0].nodeId === spec.id, 'parent collapse did not produce one outer presentation range')
  assertYamlFolding(!specPresentation.controls.some((node) => node.id === env.id), 'nested controls remained visible inside a collapsed parent')

  const bothCollapsed = toggleYamlFold(specCollapsed, env.id)
  const bothPresentation = buildYamlFoldPresentation(model, bothCollapsed)
  assertYamlFolding(bothPresentation.ranges.length === 1 && bothPresentation.ranges[0].nodeId === spec.id, 'nested collapse state incorrectly replaced the parent state')

  const envOnly = toggleYamlFold(bothCollapsed, spec.id)
  const envPresentation = buildYamlFoldPresentation(model, envOnly)
  assertYamlFolding(envPresentation.ranges.length === 1 && envPresentation.ranges[0].nodeId === env.id, 'expanding the parent did not preserve the independent nested fold state')
  assertYamlFolding(envPresentation.controls.some((node) => node.id === spec.id) && envPresentation.controls.some((node) => node.id === env.id), 'visible parent and nested controls were not restored independently')
}

/** Exercises malformed and over-budget drafts without exposing unsafe partial fold trees. */
function testUnreliableYamlFallbacks(): void {
  const malformed = buildYamlFoldModel('spec:\n  containers:\n    - name: app\n      env: [\n')
  assertYamlFolding(malformed.status === 'invalid' && malformed.nodes.length === 0, 'malformed in-progress YAML exposed a partial fold tree')

  const oversized = buildYamlFoldModel('x'.repeat(YAML_FOLDING_MAX_CHARACTERS + 1))
  assertYamlFolding(oversized.status === 'bounded' && oversized.nodes.length === 0, 'over-budget YAML was parsed into an unbounded fold tree')
}

/** Exercises keyboard-facing labels, controlled-range metadata, and masked preview boundaries. */
function testYamlFoldAccessibilityAndMasking(): void {
  const source = 'data:\n  token: [redacted]\nmetadata:\n  labels:\n    app: safe\n'
  const model = buildYamlFoldModel(source)
  const data = findYamlFoldNode(model.nodes, 'mapping', 'data')
  const expanded = yamlFoldControl(data, false)
  const collapsed = yamlFoldControl(data, true)
  assertYamlFolding(expanded.ariaExpanded && !collapsed.ariaExpanded, 'fold accessibility state did not invert with collapse state')
  assertYamlFolding(expanded.ariaLabel.includes('Collapse nested children of mapping key data'), 'mapping fold control did not expose its controlled key')
  assertYamlFolding(expanded.rangeId.startsWith('yaml-fold-range-') && expanded.controlId.startsWith('yaml-fold-control-'), 'fold controls did not expose stable controlled DOM identifiers')

  const presentation = buildYamlFoldPresentation(model, new Set([data.id]))
  assertYamlFolding(!JSON.stringify(presentation).includes('[redacted]'), 'collapsed preview metadata exposed a Secret placeholder value')
}

/** Exercises source identity and serialized draft preservation across all fold operations. */
function testSerializedDraftPreservation(): void {
  const source = 'spec:\n  settings:\n    enabled: true\n    nested:\n      value: unchanged\n'
  const model = buildYamlFoldModel(source)
  const settings = findYamlFoldNode(model.nodes, 'mapping', 'settings')
  const state = toggleYamlFold(new Set(), settings.id)
  buildYamlFoldPresentation(model, state)
  assertYamlFolding(model.source === source, 'fold model did not retain the exact source draft')
  assertYamlFolding(source === 'spec:\n  settings:\n    enabled: true\n    nested:\n      value: unchanged\n', 'folding changed the serialized source draft')
}

/** Runs deterministic model coverage for the U17 foldable YAML editor structure. */
export function runYamlFoldingModelTests(): void {
  testNestedYamlStructures()
  testIndependentNestedFoldState()
  testUnreliableYamlFallbacks()
  testYamlFoldAccessibilityAndMasking()
  testSerializedDraftPreservation()
}

runYamlFoldingModelTests()
