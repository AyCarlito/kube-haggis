import { parseDocument, stringify } from 'yaml'

/** Identifies the operator-visible state of one local YAML comparison. */
export type YamlDiffStatus = 'clean' | 'changed' | 'invalid' | 'bounded' | 'unavailable' | 'computing'

/** Identifies the line-level relationship represented by one diff row. */
export type YamlDiffRowKind = 'context' | 'added' | 'removed' | 'changed'

/** Contains one YAML line and its one-based source line number. */
export interface YamlDiffLine {
  lineNumber: number
  text: string
}

/** Pairs before and after lines without changing either source document. */
export interface YamlDiffRow {
  kind: YamlDiffRowKind
  before?: YamlDiffLine
  after?: YamlDiffLine
}

/** Summarizes the visible line changes in a bounded comparison. */
export interface YamlDiffSummary {
  additions: number
  removals: number
  changes: number
}

/** Contains a read-only, browser-local YAML comparison result. */
export interface YamlDiffModel {
  status: YamlDiffStatus
  message: string
  rows: YamlDiffRow[]
  beforeLineCount: number
  afterLineCount: number
  summary: YamlDiffSummary
}

/** Stores the last accepted cluster snapshot used by the live draft diff. */
export interface YamlDiffBaseline {
  text: string
}

/** Selects the responsive diff layout breakpoint shared by the model and CSS. */
export const YAML_DIFF_DESKTOP_BREAKPOINT = 760

/** Caps the source characters examined by one browser-local comparison. */
export const YAML_DIFF_MAX_CHARACTERS = 160_000

/** Caps the lines examined by one browser-local comparison. */
export const YAML_DIFF_MAX_LINES = 2_500

/** Caps Myers diff work so pathological edits resolve to an explicit bounded state. */
export const YAML_DIFF_MAX_WORK = 400_000

/** Creates a baseline snapshot that can be replaced only after an accepted synchronization. */
export function createYamlDiffBaseline(text: string): YamlDiffBaseline {
  return { text }
}

/** Retains a baseline after a failed refresh or replaces it after a successful synchronization. */
export function updateYamlDiffBaseline(current: YamlDiffBaseline, next: YamlDiffBaseline, accepted: boolean): YamlDiffBaseline {
  return accepted ? next : current
}

/** Creates an explicit local-computation state before asynchronous diff work completes. */
export function createYamlDiffComputingModel(): YamlDiffModel {
  return emptyYamlDiffModel('computing', 'Calculating a local YAML diff.')
}

/** Creates an explicit no-snapshot state for new resources or not-yet-loaded details. */
export function createYamlDiffUnavailableModel(): YamlDiffModel {
  return emptyYamlDiffModel('unavailable', 'No synchronized cluster snapshot is available for this resource yet.')
}

/** Chooses side-by-side presentation for desktop widths and unified presentation for narrow widths. */
export function yamlDiffLayoutForWidth(width: number): 'side-by-side' | 'unified' {
  return width > YAML_DIFF_DESKTOP_BREAKPOINT ? 'side-by-side' : 'unified'
}

/** Builds a bounded line-level diff from the synchronized snapshot to the local draft. */
export function buildYamlDiff(baseline: string, draft: string): YamlDiffModel {
  if (!baseline.trim()) return createYamlDiffUnavailableModel()
  const baselineLineCount = yamlLines(baseline).length
  const draftLineCount = yamlLines(draft).length
  if (isYamlDiffBounded(baseline, draft, baselineLineCount, draftLineCount)) {
    return emptyYamlDiffModel('bounded', 'This YAML document is too large or complex for a bounded local diff. The editor text is unchanged.')
  }

  const before = prepareYamlDiffSource(baseline)
  if (!before.valid) return emptyYamlDiffModel('unavailable', 'The synchronized cluster snapshot is not a valid resource document.')

  const after = prepareYamlDiffSource(draft)
  const beforeDisplayLines = yamlLines(before.display)
  const afterDisplayLines = yamlLines(after.display)
  if (!after.valid) {
    return {
      status: 'invalid',
      message: 'The local draft is incomplete or invalid. The editor text is unchanged; fix it to inspect a line diff.',
      rows: [],
      beforeLineCount: beforeDisplayLines.length,
      afterLineCount: afterDisplayLines.length,
      summary: emptyYamlDiffSummary(),
    }
  }

  const beforeComparisonLines = yamlLines(before.comparison)
  const afterComparisonLines = yamlLines(after.comparison)
  const operations = diffYamlLines(beforeComparisonLines, afterComparisonLines)
  if (!operations) return emptyYamlDiffModel('bounded', 'This edit would exceed the bounded local diff budget. The editor text is unchanged.')

  const rows = buildYamlDiffRows(operations, beforeDisplayLines, afterDisplayLines)
  const summary = summarizeYamlDiffRows(rows)
  return {
    status: summary.additions || summary.removals || summary.changes ? 'changed' : 'clean',
    message: summary.additions || summary.removals || summary.changes
      ? `${summary.additions} addition${summary.additions === 1 ? '' : 's'}, ${summary.removals} removal${summary.removals === 1 ? '' : 's'}, ${summary.changes} changed line${summary.changes === 1 ? '' : 's'}.`
      : 'The local draft matches the last synchronized cluster snapshot.',
    rows,
    beforeLineCount: beforeDisplayLines.length,
    afterLineCount: afterDisplayLines.length,
    summary,
  }
}

/** Creates a status model without exposing any source text when comparison cannot proceed. */
function emptyYamlDiffModel(status: YamlDiffStatus, message: string): YamlDiffModel {
  return {
    status,
    message,
    rows: [],
    beforeLineCount: 0,
    afterLineCount: 0,
    summary: emptyYamlDiffSummary(),
  }
}

/** Creates the zero-count summary shared by non-diffable comparison states. */
function emptyYamlDiffSummary(): YamlDiffSummary {
  return { additions: 0, removals: 0, changes: 0 }
}

/** Represents source text after parser validation and display-only sanitization. */
interface PreparedYamlDiffSource {
  valid: boolean
  display: string
  comparison: string
}

/** Parses and sanitizes one source without allowing invalid text into rendered diff rows. */
function prepareYamlDiffSource(source: string): PreparedYamlDiffSource {
  const parsed = parseYamlObject(source)
  if (!parsed) return { valid: false, display: source, comparison: source }

  const sanitized = sanitizeYamlObject(parsed)
  const hasManagedFields = isRecord(parsed.metadata) && Object.prototype.hasOwnProperty.call(parsed.metadata, 'managedFields')
  const display = hasManagedFields ? stringify(sanitized) : source
  return { valid: true, display, comparison: display }
}

/** Parses only plain YAML resource objects and rejects empty, scalar, and sequence drafts. */
function parseYamlObject(source: string): Record<string, unknown> | null {
  if (!source.trim() || source.length > YAML_DIFF_MAX_CHARACTERS) return null
  try {
    const document = parseDocument(source)
    if (document.errors.length > 0) return null
    const value = document.toJS() as unknown
    return isRecord(value) ? value : null
  } catch {
    return null
  }
}

/** Clones a parsed resource and removes server-owned metadata before comparison. */
function sanitizeYamlObject(value: unknown): Record<string, unknown> {
  const copy = cloneYamlValue(value)
  if (!isRecord(copy)) return {}
  if (isRecord(copy.metadata)) delete copy.metadata.managedFields
  return copy
}

/** Clones JSON-compatible YAML values without retaining mutable parser state. */
function cloneYamlValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map((item) => cloneYamlValue(item))
  if (isRecord(value)) return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, cloneYamlValue(item)]))
  return value
}

/** Identifies the plain object shape accepted as a Kubernetes resource document. */
function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

/** Splits YAML into display lines while omitting only the synthetic final line after a trailing newline. */
function yamlLines(source: string): string[] {
  const lines = source.replace(/\r\n?/g, '\n').split('\n')
  if (lines.length > 1 && lines[lines.length - 1] === '') lines.pop()
  return lines.length ? lines : ['']
}

/** Returns whether source size or line count exceeds the local diff budget. */
function isYamlDiffBounded(before: string, after: string, beforeLineCount: number, afterLineCount: number): boolean {
  return before.length > YAML_DIFF_MAX_CHARACTERS
    || after.length > YAML_DIFF_MAX_CHARACTERS
    || beforeLineCount > YAML_DIFF_MAX_LINES
    || afterLineCount > YAML_DIFF_MAX_LINES
}

/** Identifies one atomic Myers edit between the comparison line sequences. */
type YamlDiffOperationKind = 'equal' | 'insert' | 'delete'

/** Stores source indexes for one atomic line edit. */
interface YamlDiffOperation {
  kind: YamlDiffOperationKind
  beforeIndex: number
  afterIndex: number
}

/** Computes a bounded Myers edit path without changing either input sequence. */
function diffYamlLines(before: string[], after: string[]): YamlDiffOperation[] | null {
  const trace: Array<Map<number, number>> = []
  let frontier = new Map<number, number>([[1, 0]])
  let work = 0
  const maximumDepth = before.length + after.length

  for (let depth = 0; depth <= maximumDepth; depth += 1) {
    trace.push(new Map(frontier))
    for (let diagonal = -depth; diagonal <= depth; diagonal += 2) {
      work += 1
      if (work > YAML_DIFF_MAX_WORK) return null
      const down = diagonal === -depth || (diagonal !== depth && (frontier.get(diagonal - 1) ?? -1) < (frontier.get(diagonal + 1) ?? -1))
      let beforeIndex = down ? (frontier.get(diagonal + 1) ?? 0) : (frontier.get(diagonal - 1) ?? 0) + 1
      let afterIndex = beforeIndex - diagonal
      while (beforeIndex < before.length && afterIndex < after.length && before[beforeIndex] === after[afterIndex]) {
        beforeIndex += 1
        afterIndex += 1
        work += 1
        if (work > YAML_DIFF_MAX_WORK) return null
      }
      frontier.set(diagonal, beforeIndex)
      if (beforeIndex >= before.length && afterIndex >= after.length) return backtrackYamlDiff(trace, before.length, after.length)
    }
  }
  return null
}

/** Reconstructs one Myers edit path from its bounded frontier trace. */
function backtrackYamlDiff(trace: Array<Map<number, number>>, beforeLength: number, afterLength: number): YamlDiffOperation[] {
  const operations: YamlDiffOperation[] = []
  let beforeIndex = beforeLength
  let afterIndex = afterLength

  for (let depth = trace.length - 1; depth > 0; depth -= 1) {
    const previous = trace[depth]
    const diagonal = beforeIndex - afterIndex
    const down = diagonal === -depth || (diagonal !== depth && (previous.get(diagonal - 1) ?? -1) < (previous.get(diagonal + 1) ?? -1))
    const previousDiagonal = down ? diagonal + 1 : diagonal - 1
    const previousBeforeIndex = previous.get(previousDiagonal) ?? 0
    const previousAfterIndex = previousBeforeIndex - previousDiagonal
    while (beforeIndex > previousBeforeIndex && afterIndex > previousAfterIndex) {
      operations.push({ kind: 'equal', beforeIndex: beforeIndex - 1, afterIndex: afterIndex - 1 })
      beforeIndex -= 1
      afterIndex -= 1
    }
    if (beforeIndex === previousBeforeIndex) {
      operations.push({ kind: 'insert', beforeIndex, afterIndex: afterIndex - 1 })
      afterIndex -= 1
    } else {
      operations.push({ kind: 'delete', beforeIndex: beforeIndex - 1, afterIndex })
      beforeIndex -= 1
    }
  }
  while (beforeIndex > 0 && afterIndex > 0) {
    operations.push({ kind: 'equal', beforeIndex: beforeIndex - 1, afterIndex: afterIndex - 1 })
    beforeIndex -= 1
    afterIndex -= 1
  }
  while (beforeIndex > 0) {
    operations.push({ kind: 'delete', beforeIndex: beforeIndex - 1, afterIndex })
    beforeIndex -= 1
  }
  while (afterIndex > 0) {
    operations.push({ kind: 'insert', beforeIndex, afterIndex: afterIndex - 1 })
    afterIndex -= 1
  }
  return operations.reverse()
}

/** Converts atomic edits into paired changed rows and unpaired additions or removals. */
function buildYamlDiffRows(operations: YamlDiffOperation[], before: string[], after: string[]): YamlDiffRow[] {
  const rows: YamlDiffRow[] = []
  let index = 0
  while (index < operations.length) {
    const operation = operations[index]
    if (operation.kind === 'equal') {
      rows.push({ kind: 'context', before: yamlDiffLine(before, operation.beforeIndex), after: yamlDiffLine(after, operation.afterIndex) })
      index += 1
      continue
    }

    const removed: YamlDiffOperation[] = []
    const added: YamlDiffOperation[] = []
    while (index < operations.length && operations[index].kind !== 'equal') {
      const candidate = operations[index]
      if (candidate.kind === 'delete') removed.push(candidate)
      else added.push(candidate)
      index += 1
    }
    rows.push(...pairYamlDiffOperations(removed, added, before, after))
  }
  return rows
}

/** Pairs replacement lines by YAML key so nearby nested additions are not mislabeled as changes. */
function pairYamlDiffOperations(removed: YamlDiffOperation[], added: YamlDiffOperation[], before: string[], after: string[]): YamlDiffRow[] {
  const rows: YamlDiffRow[] = []
  const remaining = [...added]
  for (const removedOperation of removed) {
    const removedIdentity = yamlLineIdentity(before[removedOperation.beforeIndex] || '')
    const matchIndex = remaining.findIndex((candidate) => {
      const addedIdentity = yamlLineIdentity(after[candidate.afterIndex] || '')
      return removedIdentity === addedIdentity
    })
    if (matchIndex < 0) {
      rows.push({ kind: 'removed', before: yamlDiffLine(before, removedOperation.beforeIndex) })
      continue
    }
    const leadingAdditions = remaining.splice(0, matchIndex)
    for (const addition of leadingAdditions) rows.push({ kind: 'added', after: yamlDiffLine(after, addition.afterIndex) })
    const matchingAddition = remaining.shift()!
    rows.push({ kind: 'changed', before: yamlDiffLine(before, removedOperation.beforeIndex), after: yamlDiffLine(after, matchingAddition.afterIndex) })
  }
  for (const addition of remaining) rows.push({ kind: 'added', after: yamlDiffLine(after, addition.afterIndex) })
  return rows
}

/** Extracts a stable YAML mapping key for safe changed-line pairing. */
function yamlLineIdentity(line: string): string | undefined {
  const match = line.match(/^\s*(?:-\s+)?(?:['"]([^'"]+)['"]|([^:#][^:]*?))\s*:/)
  return match ? (match[1] ?? match[2] ?? '').trim() : undefined
}

/** Converts a safe source line index into a renderable one-based line record. */
function yamlDiffLine(lines: string[], index: number): YamlDiffLine {
  return { lineNumber: index + 1, text: lines[index] ?? '' }
}

/** Counts additions, removals, and changed line pairs in a rendered diff. */
function summarizeYamlDiffRows(rows: YamlDiffRow[]): YamlDiffSummary {
  return rows.reduce<YamlDiffSummary>((summary, row) => {
    if (row.kind === 'added') summary.additions += 1
    if (row.kind === 'removed') summary.removals += 1
    if (row.kind === 'changed') summary.changes += 1
    return summary
  }, emptyYamlDiffSummary())
}
