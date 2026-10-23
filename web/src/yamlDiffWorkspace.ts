import { YAML_DIFF_DESKTOP_BREAKPOINT } from './yamlDiff'

/** Identifies the presentation mode selected by the available workspace width. */
export type YamlDiffWorkspaceMode = 'desktop' | 'narrow'

/** Stores presentation-only state for the integrated YAML editor workspace. */
export interface YamlDiffWorkspaceState {
  open: boolean
  splitPercent: number
}

/** Describes one presentation-only workspace state transition. */
export type YamlDiffWorkspaceAction =
  | { type: 'toggle' }
  | { type: 'set-open'; open: boolean }
  | { type: 'resize'; splitPercent: number }

/** Contains the stable accessible names used by the editor, diff, toggle, and divider. */
export interface YamlDiffWorkspaceAccessibility {
  workspaceLabel: string
  editorPaneLabel: string
  diffPaneLabel: string
  toggleLabel: string
  closeLabel: string
  dividerLabel: string
}

/** Defines the default editor-to-diff split used when a workspace first opens. */
export const YAML_DIFF_WORKSPACE_DEFAULT_SPLIT = 50

/** Defines the smallest editor or diff pane allowed by the draggable divider. */
export const YAML_DIFF_WORKSPACE_MIN_SPLIT = 30

/** Defines the largest editor or diff pane allowed by the draggable divider. */
export const YAML_DIFF_WORKSPACE_MAX_SPLIT = 70

/** Provides the stable DOM identifier shared by the live-diff toggle and pane. */
export const YAML_DIFF_WORKSPACE_PANE_ID = 'yaml-diff-workspace-pane'

/** Selects desktop split panes above the shared responsive breakpoint. */
export function yamlDiffWorkspaceModeForWidth(width: number): YamlDiffWorkspaceMode {
  return width > YAML_DIFF_DESKTOP_BREAKPOINT ? 'desktop' : 'narrow'
}

/** Builds accessible workspace names without exposing draft or Secret content. */
export function yamlDiffWorkspaceAccessibility(open: boolean): YamlDiffWorkspaceAccessibility {
  return {
    workspaceLabel: 'YAML editor and live diff workspace',
    editorPaneLabel: 'Editable local-draft YAML',
    diffPaneLabel: 'Read-only live YAML diff',
    toggleLabel: open ? 'Hide live diff' : 'Show live diff',
    closeLabel: 'Close live YAML diff',
    dividerLabel: 'Resize editor and live diff panes',
  }
}

/** Identifies the Escape key used to close the narrow live-diff drawer. */
export function isYamlDiffWorkspaceEscapeKey(key: string): boolean {
  return key === 'Escape'
}

/** Keeps a requested desktop split inside usable editor and diff bounds. */
export function clampYamlDiffWorkspaceSplit(splitPercent: number): number {
  if (!Number.isFinite(splitPercent)) return YAML_DIFF_WORKSPACE_DEFAULT_SPLIT
  return Math.min(YAML_DIFF_WORKSPACE_MAX_SPLIT, Math.max(YAML_DIFF_WORKSPACE_MIN_SPLIT, splitPercent))
}

/** Creates presentation state without changing any editor, draft, or review state. */
export function createYamlDiffWorkspaceState(open = false): YamlDiffWorkspaceState {
  return { open, splitPercent: YAML_DIFF_WORKSPACE_DEFAULT_SPLIT }
}

/** Applies a workspace-only transition while retaining the current pane split. */
export function reduceYamlDiffWorkspaceState(state: YamlDiffWorkspaceState, action: YamlDiffWorkspaceAction): YamlDiffWorkspaceState {
  if (action.type === 'toggle') return { ...state, open: !state.open }
  if (action.type === 'set-open') return { ...state, open: action.open }
  return { ...state, splitPercent: clampYamlDiffWorkspaceSplit(action.splitPercent) }
}
