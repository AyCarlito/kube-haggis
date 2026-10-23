import {
  buildYamlFoldModel,
  findYamlFoldRangeByOffsets,
  findYamlFoldRangeForLine,
  mapYamlFoldNodeToSourceRange,
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

/** Exercises conversion from parser line ranges to physical CodeMirror source offsets. */
function testPhysicalYamlFoldRanges(): void {
  const source = 'spec:\n  containers:\n    - name: app\n      env:\n        - name: MODE\n          value: safe\n'
  const model = buildYamlFoldModel(source)
  const spec = findYamlFoldNode(model.nodes, 'mapping', 'spec')
  const env = findYamlFoldNode(model.nodes, 'mapping', 'env')
  const specRange = mapYamlFoldNodeToSourceRange(source, spec)
  const envRange = mapYamlFoldNodeToSourceRange(source, env)
  assertYamlFolding(specRange !== undefined && envRange !== undefined, 'validated fold nodes did not map to physical ranges')
  assertYamlFolding(specRange!.from === source.indexOf('  containers:'), 'mapping fold did not begin at the first child line')
  assertYamlFolding(source.slice(specRange!.from, specRange!.to) === source.slice(specRange!.from, source.lastIndexOf('value: safe') + 'value: safe'.length), 'mapping fold range did not cover the complete child source')
  assertYamlFolding(envRange!.from === source.indexOf('        - name: MODE'), 'nested sequence fold did not begin at its child line')
  assertYamlFolding(findYamlFoldRangeForLine(model, source, spec.line)?.nodeId === spec.id, 'line-based physical range lookup missed the mapping fold')
  assertYamlFolding(findYamlFoldRangeByOffsets(model, source, envRange!.from, envRange!.to)?.nodeId === env.id, 'offset-based physical range lookup missed the nested fold')
  assertYamlFolding(model.source === source, 'physical range mapping changed the serialized YAML source')
}

/** Exercises CRLF offsets without normalizing the source draft used by the editor. */
function testPhysicalYamlFoldRangePreservesLineEndings(): void {
  const source = 'spec:\r\n  nested:\r\n    enabled: true\r\n'
  const model = buildYamlFoldModel(source)
  const spec = findYamlFoldNode(model.nodes, 'mapping', 'spec')
  const range = mapYamlFoldNodeToSourceRange(source, spec)
  assertYamlFolding(range !== undefined, 'CRLF YAML did not produce a physical range')
  assertYamlFolding(source.slice(range!.from, range!.to) === '  nested:\r\n    enabled: true', 'CRLF physical range included or removed the wrong line ending')
}

/** Exercises malformed and over-budget drafts without exposing unsafe partial fold trees. */
function testUnreliableYamlFallbacks(): void {
  const malformed = buildYamlFoldModel('spec:\n  containers:\n    - name: app\n      env: [\n')
  assertYamlFolding(malformed.status === 'invalid' && malformed.nodes.length === 0, 'malformed in-progress YAML exposed a partial fold tree')

  const oversized = buildYamlFoldModel('x'.repeat(YAML_FOLDING_MAX_CHARACTERS + 1))
  assertYamlFolding(oversized.status === 'bounded' && oversized.nodes.length === 0, 'over-budget YAML was parsed into an unbounded fold tree')
}

/** Runs deterministic source-model coverage for physical CodeMirror fold ranges. */
export function runYamlFoldingModelTests(): void {
  testNestedYamlStructures()
  testPhysicalYamlFoldRanges()
  testPhysicalYamlFoldRangePreservesLineEndings()
  testUnreliableYamlFallbacks()
}

runYamlFoldingModelTests()
