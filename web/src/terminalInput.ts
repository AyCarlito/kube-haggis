/** Describes the clipboard-related terminal action selected for one keyboard event. */
export type TerminalClipboardAction = 'copy-selection' | 'paste' | 'terminal-input'

/** Defines the keyboard fields needed to classify terminal clipboard shortcuts. */
export interface TerminalKeyInput {
  type: string
  key: string
  ctrlKey: boolean
  metaKey: boolean
  shiftKey: boolean
}

/** Resolves whether one keyboard event should use the clipboard or reach the remote TTY. */
export function terminalClipboardAction(event: TerminalKeyInput, hasSelection: boolean, clipboardReadable: boolean): TerminalClipboardAction {
  if (event.type !== 'keydown') return 'terminal-input'
  const modifier = event.ctrlKey || event.metaKey
  const key = event.key.toLowerCase()
  if (modifier && key === 'c' && hasSelection) return 'copy-selection'
  if (((modifier && key === 'v') || (event.shiftKey && event.key === 'Insert')) && clipboardReadable) return 'paste'
  return 'terminal-input'
}
