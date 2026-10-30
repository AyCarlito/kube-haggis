import { terminalClipboardAction } from './terminalInput'

/** Fails a focused terminal clipboard shortcut assertion with a readable message. */
function assertTerminalInput(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Verifies copy, paste, shell interrupt, and unavailable-clipboard keyboard behavior. */
export function runTerminalInputTests(): void {
  const base = { type: 'keydown', ctrlKey: false, metaKey: false, shiftKey: false }
  assertTerminalInput(terminalClipboardAction({ ...base, key: 'c', ctrlKey: true }, true, true) === 'copy-selection', 'Ctrl+C with a selection did not select copy behavior')
  assertTerminalInput(terminalClipboardAction({ ...base, key: 'c', ctrlKey: true }, false, true) === 'terminal-input', 'Ctrl+C without a selection stopped being a remote interrupt')
  assertTerminalInput(terminalClipboardAction({ ...base, key: 'v', ctrlKey: true }, false, true) === 'paste', 'Ctrl+V did not select clipboard paste behavior')
  assertTerminalInput(terminalClipboardAction({ ...base, key: 'V', metaKey: true }, false, true) === 'paste', 'Cmd+V did not select clipboard paste behavior')
  assertTerminalInput(terminalClipboardAction({ ...base, key: 'Insert', shiftKey: true }, false, true) === 'paste', 'Shift+Insert did not select clipboard paste behavior')
  assertTerminalInput(terminalClipboardAction({ ...base, key: 'v', ctrlKey: true }, false, false) === 'terminal-input', 'Ctrl+V incorrectly blocked the terminal when clipboard access was unavailable')
}

runTerminalInputTests()
