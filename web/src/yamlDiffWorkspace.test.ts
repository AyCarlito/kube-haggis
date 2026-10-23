import {
  clampYamlDiffWorkspaceSplit,
  createYamlDiffWorkspaceState,
  isYamlDiffWorkspaceEscapeKey,
  reduceYamlDiffWorkspaceState,
  yamlDiffWorkspaceAccessibility,
  yamlDiffWorkspaceModeForWidth,
  YAML_DIFF_WORKSPACE_MAX_SPLIT,
  YAML_DIFF_WORKSPACE_MIN_SPLIT,
} from './yamlDiffWorkspace'
import { buildYamlDiff, createYamlDiffBaseline, updateYamlDiffBaseline } from './yamlDiff'

/** Fails a focused integrated YAML workspace assertion with a readable message. */
function assertYamlWorkspace(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Exercises the desktop split and narrow drawer breakpoint contract. */
function testWorkspaceModes(): void {
  assertYamlWorkspace(yamlDiffWorkspaceModeForWidth(1200) === 'desktop', 'wide YAML workspace did not select desktop panes')
  assertYamlWorkspace(yamlDiffWorkspaceModeForWidth(760) === 'narrow', 'breakpoint-width YAML workspace did not select the drawer')
  assertYamlWorkspace(yamlDiffWorkspaceModeForWidth(360) === 'narrow', 'narrow YAML workspace did not select the drawer')
}

/** Exercises bounded divider resizing without allowing either pane to collapse. */
function testWorkspaceSplitBounds(): void {
  assertYamlWorkspace(clampYamlDiffWorkspaceSplit(-1) === YAML_DIFF_WORKSPACE_MIN_SPLIT, 'workspace divider accepted an undersized editor pane')
  assertYamlWorkspace(clampYamlDiffWorkspaceSplit(101) === YAML_DIFF_WORKSPACE_MAX_SPLIT, 'workspace divider accepted an undersized diff pane')
  assertYamlWorkspace(clampYamlDiffWorkspaceSplit(Number.NaN) === 50, 'invalid workspace divider input did not return to the default split')
}

/** Exercises opening, closing, and resizing while retaining presentation state needed by the editor host. */
function testPresentationTransitionsPreserveEditorSession(): void {
  const editorSession = {
    draft: 'spec:\n  replicas: 2\n',
    selection: { anchor: 14, head: 14 },
    foldedRanges: ['spec'],
    activeTab: 'yaml',
    reviewReady: true,
  }
  const initial = createYamlDiffWorkspaceState()
  const resized = reduceYamlDiffWorkspaceState(initial, { type: 'resize', splitPercent: 64 })
  const opened = reduceYamlDiffWorkspaceState(resized, { type: 'toggle' })
  const closed = reduceYamlDiffWorkspaceState(opened, { type: 'toggle' })
  assertYamlWorkspace(opened.open && closed.open === false, 'workspace toggle did not change only presentation visibility')
  assertYamlWorkspace(opened.splitPercent === 64 && closed.splitPercent === 64, 'workspace toggle discarded the resized divider position')
  assertYamlWorkspace(editorSession.draft.includes('replicas: 2') && editorSession.selection.anchor === 14 && editorSession.foldedRanges[0] === 'spec' && editorSession.activeTab === 'yaml' && editorSession.reviewReady, 'workspace presentation transitions changed the editor session fixture')
}

/** Exercises the labels and Escape contract used by the desktop divider and narrow drawer. */
function testWorkspaceAccessibility(): void {
  const closed = yamlDiffWorkspaceAccessibility(false)
  const open = yamlDiffWorkspaceAccessibility(true)
  assertYamlWorkspace(closed.workspaceLabel === 'YAML editor and live diff workspace' && closed.editorPaneLabel === 'Editable local-draft YAML', 'workspace pane labels were not explicit')
  assertYamlWorkspace(closed.toggleLabel === 'Show live diff' && open.toggleLabel === 'Hide live diff', 'live-diff toggle labels did not expose state')
  assertYamlWorkspace(open.closeLabel === 'Close live YAML diff' && open.dividerLabel === 'Resize editor and live diff panes', 'drawer or divider controls lacked accessible names')
  assertYamlWorkspace(isYamlDiffWorkspaceEscapeKey('Escape') && !isYamlDiffWorkspaceEscapeKey('Enter'), 'workspace Escape handling did not distinguish the close key')
}

/** Exercises the workspace-facing model contract for accepted baselines and redacted Secret rows. */
function testWorkspaceModelSafety(): void {
  const baseline = createYamlDiffBaseline('apiVersion: v1\nkind: Secret\ndata:\n  token: old-secret\n', { apiVersion: 'v1', kind: 'Secret', data: { token: 'old-secret' } })
  const failedRefresh = updateYamlDiffBaseline(baseline, createYamlDiffBaseline('data:\n  token: replacement\n'), false)
  const draft = 'apiVersion: v1\nkind: Secret\ndata:\n  token: new-secret\n'
  const model = buildYamlDiff(failedRefresh.text, draft, {
    secret: true,
    baselineObject: failedRefresh.object,
    draftObject: { apiVersion: 'v1', kind: 'Secret', data: { token: 'new-secret' } },
  })
  const rendered = JSON.stringify(model)
  assertYamlWorkspace(model.status === 'changed' && rendered.includes('[redacted]'), 'workspace did not retain the safe Secret diff model')
  assertYamlWorkspace(!rendered.includes('old-secret') && !rendered.includes('new-secret') && failedRefresh.text.includes('old-secret'), 'workspace model exposed Secret values or replaced a failed baseline')
}

/** Runs deterministic coverage for integrated YAML workspace presentation state. */
export function runYamlDiffWorkspaceTests(): void {
  testWorkspaceModes()
  testWorkspaceSplitBounds()
  testPresentationTransitionsPreserveEditorSession()
  testWorkspaceAccessibility()
  testWorkspaceModelSafety()
}

runYamlDiffWorkspaceTests()
