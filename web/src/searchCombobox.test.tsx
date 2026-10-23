import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { SearchCombobox } from './searchCombobox'
import type { SearchComboboxOption } from './searchCombobox'

/** Fails a focused searchable-combobox rendering assertion with a readable message. */
function assertSearchCombobox(condition: boolean, message: string): void {
  if (!condition) throw new Error(message)
}

/** Verifies that global-search controls use the shared ARIA combobox surface instead of native menus. */
export function runSearchComboboxComponentTests(): void {
  const options: SearchComboboxOption[] = [
    { value: '', label: 'All groups' },
    { value: 'Core API', label: 'Core API' },
    { value: 'apps', label: 'apps' },
  ]
  const markup = renderToStaticMarkup(<SearchCombobox id="global-search-group" label="API group" value="" options={options} placeholder="All groups" onChange={() => undefined} />)
  assertSearchCombobox(markup.includes('role="combobox"') && markup.includes('aria-autocomplete="list"') && markup.includes('aria-haspopup="listbox"'), 'search controls did not render the shared ARIA combobox contract')
  assertSearchCombobox(markup.includes('Open API group suggestions') && markup.includes('All groups'), 'search combobox did not render its select-like toggle or placeholder')
  assertSearchCombobox(!markup.includes('<select') && !markup.includes('<datalist'), 'search combobox regressed to a browser-controlled menu')
}

runSearchComboboxComponentTests()
