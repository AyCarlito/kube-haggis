import {
  buildYamlDiff,
  createYamlDiffBaseline,
  updateYamlDiffBaseline,
  YAML_DIFF_MAX_LINES,
  yamlDiffComparisonAvailable,
  yamlDiffHunks,
  yamlDiffMarkers,
} from './yamlDiff'

/** Fails a focused YAML diff assertion with a readable message. */
function assertYamlDiff(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises a clean document and verifies that context lines remain available. */
function testCleanYamlDraft(): void {
  const source = 'apiVersion: v1\nspec:\n  replicas: 1\n'
  const model = buildYamlDiff(source, source)
  assertYamlDiff(model.status === 'clean', 'an unchanged YAML draft was not reported clean')
  assertYamlDiff(model.rows.length === 3 && model.rows.every((row) => row.kind === 'context'), 'clean YAML did not retain readable context lines')
}

/** Exercises nested additions, removals, and changed values as line-level rows. */
function testNestedYamlChanges(): void {
  const baseline = [
    'apiVersion: apps/v1',
    'spec:',
    '  template:',
    '    metadata:',
    '      labels:',
    '        app: old',
    '  replicas: 1',
  ].join('\n')
  const draft = [
    'apiVersion: apps/v1',
    'spec:',
    '  template:',
    '    metadata:',
    '      labels:',
    '        app: new',
    '        tier: api',
    '  replicas: 2',
  ].join('\n')
  const model = buildYamlDiff(baseline, draft)
  assertYamlDiff(model.status === 'changed', 'nested YAML edits were not reported as changed')
  assertYamlDiff(model.summary.changes === 2 && model.summary.additions === 1 && model.summary.removals === 0, 'nested changed and added lines were not classified deterministically')
  assertYamlDiff(model.rows.some((row) => row.kind === 'changed' && row.before?.text.includes('app: old') && row.after?.text.includes('app: new')), 'nested changed line pair was not retained')
  assertYamlDiff(model.rows.some((row) => row.kind === 'added' && row.after?.text.includes('tier: api')), 'nested added line was not retained')
}

/** Exercises pure removals without allowing neighboring context to absorb the deletion. */
function testRemovedYamlLines(): void {
  const model = buildYamlDiff('spec:\n  replicas: 1\n  strategy: Recreate\n', 'spec:\n  replicas: 1\n')
  assertYamlDiff(model.status === 'changed' && model.summary.removals === 1, 'removed YAML line was not classified as a removal')
  assertYamlDiff(model.rows.some((row) => row.kind === 'removed' && row.before?.text.includes('strategy: Recreate')), 'removed YAML context was not retained')
}

/** Exercises live recalculation semantics by comparing successive local draft values. */
function testLiveDraftUpdates(): void {
  const baseline = 'spec:\n  replicas: 1\n'
  const changed = buildYamlDiff(baseline, 'spec:\n  replicas: 2\n')
  const clean = buildYamlDiff(baseline, baseline)
  assertYamlDiff(changed.status === 'changed' && changed.summary.changes === 1, 'live draft change did not produce a changed model')
  assertYamlDiff(clean.status === 'clean', 'live draft update back to the baseline did not return to clean')
}

/** Exercises accepted apply/refresh baseline replacement and failed-refresh retention. */
function testBaselineTransitions(): void {
  const first = createYamlDiffBaseline('spec:\n  replicas: 1\n')
  const applied = createYamlDiffBaseline('spec:\n  replicas: 2\n')
  const refreshed = createYamlDiffBaseline('spec:\n  replicas: 3\n')
  const afterApply = updateYamlDiffBaseline(first, applied, true)
  const afterFailedRefresh = updateYamlDiffBaseline(afterApply, refreshed, false)
  const afterAcceptedRefresh = updateYamlDiffBaseline(afterFailedRefresh, refreshed, true)
  assertYamlDiff(afterApply.text.includes('replicas: 2'), 'successful apply did not replace the diff baseline')
  assertYamlDiff(afterFailedRefresh.text.includes('replicas: 2'), 'failed refresh incorrectly replaced the diff baseline')
  assertYamlDiff(afterAcceptedRefresh.text.includes('replicas: 3'), 'accepted refresh did not replace the diff baseline')
}

/** Exercises malformed drafts and verifies editor text never appears in a diff row. */
function testMalformedDraft(): void {
  const model = buildYamlDiff('spec:\n  replicas: 1\n', 'spec:\n  template: [\n')
  assertYamlDiff(model.status === 'invalid' && model.rows.length === 0, 'malformed YAML did not produce an explicit incomplete state')
  assertYamlDiff(!model.message.includes('template: ['), 'malformed draft text was copied into the explanatory state')
}

/** Exercises the line budget without parsing an unbounded browser-local document. */
function testBoundedDraft(): void {
  const model = buildYamlDiff('spec:\n  replicas: 1\n', Array.from({ length: YAML_DIFF_MAX_LINES + 1 }, (_, index) => `key${index}: value`).join('\n'))
  assertYamlDiff(model.status === 'bounded' && model.rows.length === 0, 'over-budget YAML did not produce an explicit bounded state')
}

/** Exercises native comparison eligibility without treating invalid or over-budget states as editor fallbacks. */
function testNativeComparisonStates(): void {
  const baseline = 'spec:\n  replicas: 1\n'
  assertYamlDiff(yamlDiffComparisonAvailable(baseline, 'clean'), 'a clean bounded model was not comparison-ready')
  assertYamlDiff(yamlDiffComparisonAvailable(baseline, 'changed'), 'a changed bounded model was not comparison-ready')
  assertYamlDiff(yamlDiffComparisonAvailable(baseline, 'invalid'), 'an invalid draft did not retain native text comparison eligibility')
  assertYamlDiff(!yamlDiffComparisonAvailable('', 'clean') && !yamlDiffComparisonAvailable(baseline, 'bounded') && !yamlDiffComparisonAvailable(baseline, 'computing'), 'unavailable, bounded, or computing states opened a native comparison')
}

/** Exercises deterministic hunk grouping and modified-side marker ranges for changed YAML lines. */
function testHunkMarkers(): void {
  const baseline = 'spec:\n  replicas: 1\n  strategy: Recreate\n'
  const draft = 'spec:\n  replicas: 2\n  mode: Active\n'
  const model = buildYamlDiff(baseline, draft)
  const hunks = yamlDiffHunks(model)
  const markers = yamlDiffMarkers(model)
  assertYamlDiff(hunks.length === 1 && hunks[0].kind === 'changed', 'contiguous changed YAML rows were not grouped into one hunk')
  assertYamlDiff(hunks[0].originalStartLineNumber === 2 && hunks[0].modifiedStartLineNumber === 2, 'hunk anchors did not retain the changed source lines')
  assertYamlDiff(markers.length === 1 && markers[0].hunkID === hunks[0].id && markers[0].startLineNumber === 2, 'modified-side marker range did not follow the hunk')
  const removed = yamlDiffMarkers(buildYamlDiff('spec:\n  replicas: 1\n  strategy: Recreate\n', 'spec:\n  replicas: 1\n'))
  assertYamlDiff(removed.length === 1 && removed[0].startLineNumber === 2, 'a removed trailing line was not anchored to a valid modified line')
}

/** Exercises raw Secret values in the browser-local editor diff without a reveal gate. */
function testSecretEditorDiff(): void {
  const baseline = 'apiVersion: v1\nkind: Secret\ndata:\n  token: old-secret\n'
  const draft = 'apiVersion: v1\nkind: Secret\ndata:\n  token: new-secret\n'
  const model = buildYamlDiff(baseline, draft)
  const renderedText = JSON.stringify(model)
  assertYamlDiff(model.status === 'changed' && model.summary.changes === 1, 'changed Secret values did not retain a changed-line marker')
  assertYamlDiff(renderedText.includes('old-secret') && renderedText.includes('new-secret'), 'raw Secret values did not appear in the browser-local editor diff')
}

/** Exercises server-owned metadata removal before a non-Secret diff is generated. */
function testDiffSanitization(): void {
  const source = 'metadata:\n  name: api\n  managedFields:\n    - manager: kube-controller\nspec:\n  replicas: 1\n'
  const model = buildYamlDiff(source, source)
  assertYamlDiff(model.status === 'clean', 'managedFields-only source differences were not sanitized before comparison')
  assertYamlDiff(!JSON.stringify(model).includes('managedFields') && !JSON.stringify(model).includes('kube-controller'), 'server-owned metadata entered generated diff content')
}

/** Runs deterministic coverage for the U18 browser-local YAML diff model. */
export function runYamlDiffModelTests(): void {
  testCleanYamlDraft()
  testNestedYamlChanges()
  testRemovedYamlLines()
  testLiveDraftUpdates()
  testBaselineTransitions()
  testMalformedDraft()
  testBoundedDraft()
  testNativeComparisonStates()
  testHunkMarkers()
  testSecretEditorDiff()
  testDiffSanitization()
}

runYamlDiffModelTests()
