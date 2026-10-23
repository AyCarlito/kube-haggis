import { isMap, isNode, isScalar, isSeq, parseDocument, type Node, type Pair, type YAMLMap, type YAMLSeq } from 'yaml'

/** Identifies the structural kind controlled by one YAML fold button. */
export type YamlFoldNodeKind = 'mapping' | 'sequence-item'

/** Identifies whether a YAML draft has a reliable, bounded fold tree. */
export type YamlFoldStatus = 'ready' | 'empty' | 'invalid' | 'bounded'

/** Describes one safely foldable mapping value or sequence item. */
export interface YamlFoldNode {
  id: string
  kind: YamlFoldNodeKind
  label: string
  line: number
  endLine: number
  depth: number
  parentId?: string
}

/** Contains the local structural information used to present YAML folds. */
export interface YamlFoldModel {
  source: string
  status: YamlFoldStatus
  lineCount: number
  nodes: YamlFoldNode[]
}

/** Describes one contiguous child-line mask shown for a collapsed structure. */
export interface YamlFoldRange {
  nodeId: string
  rangeId: string
  startLine: number
  endLine: number
  lineCount: number
  label: string
}

/** Contains the currently visible controls and collapsed presentation ranges. */
export interface YamlFoldPresentation {
  controls: YamlFoldNode[]
  ranges: YamlFoldRange[]
}

/** Describes the accessible DOM contract for one fold control. */
export interface YamlFoldControlModel {
  controlId: string
  rangeId: string
  ariaExpanded: boolean
  ariaLabel: string
  title: string
}

/** Caps source work so folding stays local and predictable for large documents. */
export const YAML_FOLDING_MAX_CHARACTERS = 160_000

/** Caps parsed source lines so a very large single draft cannot create an unbounded tree. */
export const YAML_FOLDING_MAX_LINES = 4_000

/** Caps foldable structures retained by one local parse. */
export const YAML_FOLDING_MAX_NODES = 512

/** Builds a bounded structural model without changing the source YAML. */
export function buildYamlFoldModel(source: string): YamlFoldModel {
  const lineStarts = yamlLineStarts(source)
  const lineCount = lineStarts.length
  if (!source.trim()) return { source, status: 'empty', lineCount, nodes: [] }
  if (source.length > YAML_FOLDING_MAX_CHARACTERS || lineCount > YAML_FOLDING_MAX_LINES) {
    return { source, status: 'bounded', lineCount, nodes: [] }
  }

  try {
    const document = parseDocument(source, { keepSourceTokens: true })
    if (document.errors.length > 0 || !document.contents || (!isMap(document.contents) && !isSeq(document.contents))) {
      return { source, status: 'invalid', lineCount, nodes: [] }
    }

    const nodes: YamlFoldNode[] = []
    let budgetExceeded = false

    /** Adds one fold node only when its collection spans child lines. */
    function addNode(candidate: YamlFoldNode): string | undefined {
      if (candidate.endLine <= candidate.line) return undefined
      nodes.push(candidate)
      if (nodes.length > YAML_FOLDING_MAX_NODES) budgetExceeded = true
      return candidate.id
    }

    /** Visits nested YAML collections while preserving source order and structural paths. */
    function visitNode(node: unknown, path: string[], depth: number, parentId?: string): void {
      if (budgetExceeded || !node) return
      if (isMap(node)) {
        visitMap(node, path, depth, parentId)
        return
      }
      if (isSeq(node)) visitSequence(node, path, depth, parentId)
    }

    /** Visits mapping pairs and records only collection-valued keys as foldable structures. */
    function visitMap(map: YAMLMap, path: string[], depth: number, parentId?: string): void {
      map.items.forEach((pair, index) => {
        if (budgetExceeded || !pair.value || !isNode(pair.value)) return
        const pairLine = pairLineNumber(pair, lineStarts)
        const valueLines = collectionLineRange(pair.value, lineStarts)
        if (pairLine === undefined || !valueLines) {
          visitNode(pair.value, [...path, `m${index}`], depth + 1, parentId)
          return
        }

        const nodeId = `${path.join('/')}${path.length ? '/' : ''}m${index}`
        const foldNode = addNode({
          id: nodeId,
          kind: 'mapping',
          label: mappingLabel(pair),
          line: pairLine,
          endLine: valueLines.endLine,
          depth,
          parentId,
        })
        visitNode(pair.value, [...path, `m${index}`], depth + 1, foldNode || parentId)
      })
    }

    /** Visits sequence items and records multiline collection items as foldable structures. */
    function visitSequence(sequence: YAMLSeq, path: string[], depth: number, parentId?: string): void {
      sequence.items.forEach((item, index) => {
        if (budgetExceeded || !item || !isNode(item)) return
        const itemLine = sequenceItemLine(sequence, index, item, lineStarts)
        const itemLines = collectionLineRange(item, lineStarts)
        if (itemLine === undefined || !itemLines) {
          visitNode(item, [...path, `s${index}`], depth + 1, parentId)
          return
        }

        const nodeId = `${path.join('/')}${path.length ? '/' : ''}s${index}`
        const foldNode = addNode({
          id: nodeId,
          kind: 'sequence-item',
          label: `item ${index + 1}`,
          line: itemLine,
          endLine: itemLines.endLine,
          depth,
          parentId,
        })
        visitNode(item, [...path, `s${index}`], depth + 1, foldNode || parentId)
      })
    }

    visitNode(document.contents, [], 0)
    if (budgetExceeded) return { source, status: 'bounded', lineCount, nodes: [] }
    return { source, status: 'ready', lineCount, nodes }
  } catch {
    return { source, status: 'invalid', lineCount, nodes: [] }
  }
}

/** Toggles one local fold identifier without touching the serialized YAML source. */
export function toggleYamlFold(collapsedIds: ReadonlySet<string>, nodeId: string): Set<string> {
  const next = new Set(collapsedIds)
  if (next.has(nodeId)) next.delete(nodeId)
  else next.add(nodeId)
  return next
}

/** Builds the visible controls and non-mutating masks for the requested fold state. */
export function buildYamlFoldPresentation(model: YamlFoldModel, collapsedIds: ReadonlySet<string>): YamlFoldPresentation {
  if (model.status !== 'ready' || model.nodes.length === 0) return { controls: [], ranges: [] }
  const collapsedNodes = model.nodes.filter((node) => collapsedIds.has(node.id))
  const ranges = collapsedNodes
    .filter((node) => !isLineHiddenByOtherNode(node.line, node, collapsedNodes))
    .map((node) => ({
      nodeId: node.id,
      rangeId: yamlFoldRangeId(node.id),
      startLine: node.line + 1,
      endLine: node.endLine,
      lineCount: node.endLine - node.line,
      label: node.label,
    }))
  const controls = model.nodes.filter((node) => !isLineHiddenByOtherNode(node.line, undefined, collapsedNodes))
  return { controls, ranges }
}

/** Creates keyboard and screen-reader metadata for a fold button without exposing child values. */
export function yamlFoldControl(node: YamlFoldNode, collapsed: boolean): YamlFoldControlModel {
  const target = node.kind === 'mapping' ? `mapping key ${node.label}` : `sequence ${node.label}`
  const action = collapsed ? 'Expand' : 'Collapse'
  return {
    controlId: yamlFoldControlId(node.id),
    rangeId: yamlFoldRangeId(node.id),
    ariaExpanded: !collapsed,
    ariaLabel: `${action} nested children of ${target}`,
    title: `${action} ${target}`,
  }
}

/** Returns the source line starts used to translate parser offsets to zero-based editor lines. */
function yamlLineStarts(source: string): number[] {
  const starts = [0]
  for (let index = 0; index < source.length; index += 1) {
    if (source[index] === '\n') starts.push(index + 1)
    else if (source[index] === '\r' && source[index + 1] !== '\n') starts.push(index + 1)
  }
  return starts
}

/** Converts one source offset to a zero-based YAML editor line. */
function yamlLineAtOffset(offset: number, lineStarts: number[]): number {
  const boundedOffset = Math.max(0, offset)
  let low = 0
  let high = lineStarts.length - 1
  while (low <= high) {
    const middle = Math.floor((low + high) / 2)
    if (lineStarts[middle] <= boundedOffset) low = middle + 1
    else high = middle - 1
  }
  return Math.max(0, high)
}

/** Returns the line range covered by a parsed YAML collection when its source range is usable. */
function collectionLineRange(node: unknown, lineStarts: number[]): { startLine: number; endLine: number } | undefined {
  if ((!isMap(node) && !isSeq(node)) || !node.range || node.range.length < 3) return undefined
  const start = node.range[0]
  const end = node.range[2]
  if (!Number.isInteger(start) || !Number.isInteger(end) || end <= start) return undefined
  return { startLine: yamlLineAtOffset(start, lineStarts), endLine: yamlLineAtOffset(end - 1, lineStarts) }
}

/** Returns the source line containing a mapping key or its safe parser token fallback. */
function pairLineNumber(pair: Pair, lineStarts: number[]): number | undefined {
  if (isNode(pair.key) && pair.key.range && Number.isInteger(pair.key.range[0])) return yamlLineAtOffset(pair.key.range[0], lineStarts)
  const tokenOffset = pair.srcToken?.start.find((token) => Number.isInteger(token.offset))?.offset
  return tokenOffset === undefined ? undefined : yamlLineAtOffset(tokenOffset, lineStarts)
}

/** Returns the source line containing a sequence marker, including incomplete inline items. */
function sequenceItemLine(sequence: YAMLSeq, index: number, item: Node, lineStarts: number[]): number | undefined {
  const sourceToken = sequence.srcToken
  if (sourceToken?.type === 'block-seq') {
    const marker = sourceToken.items[index]?.start.find((token) => token.type === 'seq-item-ind')
    if (marker) return yamlLineAtOffset(marker.offset, lineStarts)
  }
  if (item.range && Number.isInteger(item.range[0])) return yamlLineAtOffset(item.range[0], lineStarts)
  return undefined
}

/** Returns a display-safe mapping label that never reads a mapping value. */
function mappingLabel(pair: Pair): string {
  if (isScalar(pair.key) && pair.key.value !== undefined) return String(pair.key.value)
  return 'mapping key'
}

/** Tests whether a fold line is inside another active collapsed structure. */
function isLineHiddenByOtherNode(line: number, node: YamlFoldNode | undefined, collapsedNodes: YamlFoldNode[]): boolean {
  return collapsedNodes.some((candidate) => candidate !== node && candidate.line < line && line <= candidate.endLine)
}

/** Converts a structural node identifier into a stable safe DOM identifier. */
function yamlFoldControlId(nodeId: string): string {
  return `yaml-fold-control-${nodeId.replace(/[^a-zA-Z0-9_-]/g, '-')}`
}

/** Converts a structural node identifier into the controlled range DOM identifier. */
function yamlFoldRangeId(nodeId: string): string {
  return `yaml-fold-range-${nodeId.replace(/[^a-zA-Z0-9_-]/g, '-')}`
}
