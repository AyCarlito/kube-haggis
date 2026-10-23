import {
  isYamlDiffWorkspaceEscapeKey,
  yamlDiffWorkspaceAccessibility,
  yamlDiffWorkspaceComparisonAvailable,
  yamlDiffWorkspaceNativeComparisonAvailable,
  yamlDiffWorkspaceModeForWidth,
} from './yamlDiffWorkspace'
import { buildYamlDiff, createYamlDiffBaseline, updateYamlDiffBaseline } from './yamlDiff'

/** Fails a focused integrated YAML workspace assertion with a readable message. */
function assertYamlWorkspace(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises the desktop native split and narrow anchored-drawer breakpoint contract. */
function testWorkspaceModes(): void {
  assertYamlWorkspace(yamlDiffWorkspaceModeForWidth(1200) === 'desktop', 'wide YAML workspace did not select the native desktop diff')
  assertYamlWorkspace(yamlDiffWorkspaceModeForWidth(760) === 'narrow', 'breakpoint-width YAML workspace did not select the anchored drawer')
  assertYamlWorkspace(yamlDiffWorkspaceModeForWidth(360) === 'narrow', 'narrow YAML workspace did not select the anchored drawer')
}

/** Exercises accessible Compare labels without exposing the baseline or local draft content. */
function testWorkspaceAccessibility(): void {
  const closed = yamlDiffWorkspaceAccessibility(false)
  const open = yamlDiffWorkspaceAccessibility(true)
  assertYamlWorkspace(closed.workspaceLabel === 'YAML editor and native comparison workspace' && closed.editorPaneLabel === 'Editable local-draft YAML', 'workspace labels were not explicit')
  assertYamlWorkspace(closed.toggleLabel === 'Compare' && open.toggleLabel === 'Close compare', 'Compare action labels did not expose state')
  assertYamlWorkspace(open.closeLabel === 'Close YAML comparison' && open.diffPaneLabel.includes('original') && open.diffPaneLabel.includes('modified'), 'native comparison controls lacked accessible names')
  assertYamlWorkspace(isYamlDiffWorkspaceEscapeKey('Escape') && !isYamlDiffWorkspaceEscapeKey('Enter'), 'workspace Escape handling did not distinguish the close key')
}

/** Exercises comparison availability states used to keep the complete editable editor active. */
function testComparisonAvailability(): void {
  const baseline = 'apiVersion: v1\nkind: Secret\ndata:\n  token: old-secret\n'
  assertYamlWorkspace(yamlDiffWorkspaceComparisonAvailable(baseline, 'changed'), 'changed baseline was not eligible for native comparison')
  assertYamlWorkspace(yamlDiffWorkspaceComparisonAvailable(baseline, 'invalid'), 'invalid draft did not retain native text comparison')
  assertYamlWorkspace(!yamlDiffWorkspaceComparisonAvailable('', 'clean') && !yamlDiffWorkspaceComparisonAvailable(baseline, 'bounded'), 'comparison opened for a missing or bounded snapshot')
}

/** Exercises retention of an already-mounted native comparison during transient model computation. */
function testRetainedNativeComparisonDuringComputing(): void {
  assertYamlWorkspace(yamlDiffWorkspaceNativeComparisonAvailable(true, 'changed', true, false), 'a changed model did not open the native comparison')
  assertYamlWorkspace(yamlDiffWorkspaceNativeComparisonAvailable(false, 'computing', true, true), 'an existing native comparison was not retained during computing')
  assertYamlWorkspace(!yamlDiffWorkspaceNativeComparisonAvailable(false, 'computing', true, false), 'computing opened a native comparison without a retained editor')
  assertYamlWorkspace(!yamlDiffWorkspaceNativeComparisonAvailable(false, 'computing', false, true), 'a closed comparison retained a native editor')
  assertYamlWorkspace(!yamlDiffWorkspaceNativeComparisonAvailable(false, 'bounded', true, true) && !yamlDiffWorkspaceNativeComparisonAvailable(false, 'unavailable', true, true), 'bounded or unavailable states retained the native comparison')
}

/** Exercises accepted baselines and raw Secret values through the integrated workspace model. */
function testWorkspaceModelSafety(): void {
  const baseline = createYamlDiffBaseline('apiVersion: v1\nkind: Secret\ndata:\n  token: old-secret\n')
  const failedRefresh = updateYamlDiffBaseline(baseline, createYamlDiffBaseline('data:\n  token: replacement\n'), false)
  const draft = 'apiVersion: v1\nkind: Secret\ndata:\n  token: new-secret\n'
  const model = buildYamlDiff(failedRefresh.text, draft)
  const rendered = JSON.stringify(model)
  assertYamlWorkspace(model.status === 'changed' && rendered.includes('old-secret') && rendered.includes('new-secret'), 'workspace did not retain the raw Secret editor diff model')
  assertYamlWorkspace(failedRefresh.text.includes('old-secret') && !failedRefresh.text.includes('replacement'), 'workspace model replaced a failed baseline')
}

/** Runs deterministic coverage for native Monaco workspace presentation and state boundaries. */
export function runYamlDiffWorkspaceTests(): void {
  testWorkspaceModes()
  testWorkspaceAccessibility()
  testComparisonAvailability()
  testRetainedNativeComparisonDuringComputing()
  testWorkspaceModelSafety()
}

runYamlDiffWorkspaceTests()
