import { YAML_DIFF_DESKTOP_BREAKPOINT, yamlDiffComparisonAvailable, type YamlDiffStatus } from './yamlDiff'

/** Identifies the responsive presentation mode used by the native diff editor. */
export type YamlDiffWorkspaceMode = 'desktop' | 'narrow'

/** Contains the stable accessible names used by the editor, comparison, and close controls. */
export interface YamlDiffWorkspaceAccessibility {
  workspaceLabel: string
  editorPaneLabel: string
  diffPaneLabel: string
  toggleLabel: string
  closeLabel: string
}

/** Provides the stable DOM identifier shared by the Compare action and native diff pane. */
export const YAML_DIFF_WORKSPACE_PANE_ID = 'yaml-diff-workspace-pane'

/** Selects the anchored narrow drawer below the desktop comparison breakpoint. */
export function yamlDiffWorkspaceModeForWidth(width: number): YamlDiffWorkspaceMode {
  return width > YAML_DIFF_DESKTOP_BREAKPOINT ? 'desktop' : 'narrow'
}

/** Builds accessible comparison names without exposing draft or Secret content. */
export function yamlDiffWorkspaceAccessibility(open: boolean): YamlDiffWorkspaceAccessibility {
  return {
    workspaceLabel: 'YAML editor and native comparison workspace',
    editorPaneLabel: 'Editable local-draft YAML',
    diffPaneLabel: 'Read-only original and editable modified YAML comparison',
    toggleLabel: open ? 'Close compare' : 'Compare',
    closeLabel: 'Close YAML comparison',
  }
}

/** Returns whether a model status can open the native Monaco comparison surface. */
export function yamlDiffWorkspaceComparisonAvailable(baseline: string, status: YamlDiffStatus): boolean {
  return yamlDiffComparisonAvailable(baseline, status)
}

/** Retains an already-mounted native comparison only during transient bounded-model computation. */
export function yamlDiffWorkspaceNativeComparisonAvailable(comparisonAvailable: boolean, status: YamlDiffStatus, compareOpen: boolean, nativeComparisonReady: boolean): boolean {
  return comparisonAvailable || (compareOpen && nativeComparisonReady && status === 'computing')
}

/** Identifies the Escape key used to close the responsive comparison drawer. */
export function isYamlDiffWorkspaceEscapeKey(key: string): boolean {
  return key === 'Escape'
}
