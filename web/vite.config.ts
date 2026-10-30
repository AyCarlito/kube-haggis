import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath, URL } from 'node:url'

const monacoEditorWorkerPath = fileURLToPath(new URL('./node_modules/monaco-editor/esm/vs/editor/editor.worker.js', import.meta.url))
const monacoCodiconStylePath = fileURLToPath(new URL('./node_modules/monaco-editor/esm/vs/base/browser/ui/codicons/codicon/codicon.css', import.meta.url))
const monacoFoldingContributionPath = fileURLToPath(new URL('./node_modules/monaco-editor/esm/vs/editor/contrib/folding/browser/folding.js', import.meta.url))
const monacoClipboardContributionPath = fileURLToPath(new URL('./node_modules/monaco-editor/esm/vs/editor/contrib/clipboard/browser/clipboard.js', import.meta.url))
const monacoContextMenuContributionPath = fileURLToPath(new URL('./node_modules/monaco-editor/esm/vs/editor/contrib/contextmenu/browser/contextmenu.js', import.meta.url))
const monacoFindContributionPath = fileURLToPath(new URL('./node_modules/monaco-editor/esm/vs/editor/contrib/find/browser/findController.js', import.meta.url))
const monacoFormatContributionPath = fileURLToPath(new URL('./node_modules/monaco-editor/esm/vs/editor/contrib/format/browser/formatActions.js', import.meta.url))

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      'monaco-editor/esm/vs/base/browser/ui/codicons/codicon/codicon.css': monacoCodiconStylePath,
      'monaco-editor/esm/vs/editor/contrib/clipboard/browser/clipboard.js': monacoClipboardContributionPath,
      'monaco-editor/esm/vs/editor/contrib/contextmenu/browser/contextmenu.js': monacoContextMenuContributionPath,
      'monaco-editor/esm/vs/editor/contrib/find/browser/findController.js': monacoFindContributionPath,
      'monaco-editor/esm/vs/editor/contrib/format/browser/formatActions.js': monacoFormatContributionPath,
      'monaco-editor/esm/vs/editor/editor.worker.js': monacoEditorWorkerPath,
      'monaco-editor/esm/vs/editor/contrib/folding/browser/folding.js': monacoFoldingContributionPath,
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', ws: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
