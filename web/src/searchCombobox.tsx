import { ChevronDown } from 'lucide-react'
import React, { useEffect, useRef, useState, type FocusEvent, type KeyboardEvent, type PointerEvent as ReactPointerEvent } from 'react'

/** Describes one visible choice in the global-search combobox. */
export interface SearchComboboxOption {
  value: string
  label: string
  detail?: string
}

/** Defines the controlled value, choices, and accessibility contract for one search combobox. */
export interface SearchComboboxProps {
  id: string
  label: string
  value: string
  options: SearchComboboxOption[]
  placeholder: string
  describedBy?: string
  emptyMessage?: string
  onChange: (value: string) => void
}

/** Checks whether a choice matches the current free-form combobox text. */
function optionMatchesSearchCombobox(option: SearchComboboxOption, value: string): boolean {
  const normalized = value.trim().toLowerCase()
  if (!normalized) return true
  return [option.value, option.label, option.detail || ''].some((candidate) => candidate.toLowerCase().includes(normalized))
}

/** Finds the selected choice so the opened list starts at the current value. */
function selectedSearchComboboxIndex(options: SearchComboboxOption[], value: string): number {
  const normalized = value.trim().toLowerCase()
  const index = options.findIndex((option) => option.value.trim().toLowerCase() === normalized)
  return index >= 0 ? index : 0
}

/** Renders a select-like, keyboard-accessible, free-form global-search combobox. */
export function SearchCombobox({ id, label, value, options, placeholder, describedBy, emptyMessage = 'No catalog match. The typed value remains available.', onChange }: SearchComboboxProps) {
  const rootRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const listboxID = `${id}-listbox`
  const [open, setOpen] = useState(false)
  const [showAllOptions, setShowAllOptions] = useState(false)
  const [highlightedIndex, setHighlightedIndex] = useState(0)
  const visibleOptions = showAllOptions ? options : options.filter((option) => optionMatchesSearchCombobox(option, value))
  const activeIndex = Math.min(highlightedIndex, Math.max(visibleOptions.length - 1, 0))

  useEffect(() => {
    setHighlightedIndex((current) => Math.min(current, Math.max(visibleOptions.length - 1, 0)))
  }, [visibleOptions.length])

  useEffect(() => {
    if (!open || !visibleOptions.length) return
    document.getElementById(`${listboxID}-option-${activeIndex}`)?.scrollIntoView({ block: 'nearest' })
  }, [activeIndex, listboxID, open, visibleOptions.length])

  useEffect(() => {
    if (!open) return
    /** Closes the menu when a pointer lands outside this combobox. */
    const handleDocumentPointerDown = (event: PointerEvent) => {
      if (event.target instanceof Node && !rootRef.current?.contains(event.target)) {
        setOpen(false)
        setShowAllOptions(false)
      }
    }
    document.addEventListener('pointerdown', handleDocumentPointerDown)
    return () => document.removeEventListener('pointerdown', handleDocumentPointerDown)
  }, [open])

  /** Opens the menu and chooses whether the first view should ignore the current filter text. */
  function openSearchCombobox(showAll: boolean): void {
    setOpen(true)
    setShowAllOptions(showAll)
    setHighlightedIndex(selectedSearchComboboxIndex(showAll ? options : visibleOptions, value))
  }

  /** Toggles the styled option menu without moving focus away from its text input. */
  function toggleSearchCombobox(): void {
    if (open) {
      setOpen(false)
      setShowAllOptions(false)
      return
    }
    openSearchCombobox(true)
    inputRef.current?.focus()
  }

  /** Records free-form text and switches the menu into filtered mode. */
  function handleSearchComboboxChange(nextValue: string): void {
    setOpen(true)
    setShowAllOptions(false)
    setHighlightedIndex(0)
    onChange(nextValue)
  }

  /** Selects one visible option while preserving focus for continued keyboard use. */
  function selectSearchComboboxOption(option: SearchComboboxOption): void {
    onChange(option.value)
    setOpen(false)
    setShowAllOptions(false)
    setHighlightedIndex(0)
    inputRef.current?.focus()
  }

  /** Handles combobox navigation, selection, cancellation, and form-safe Enter behavior. */
  function handleSearchComboboxKeyDown(event: KeyboardEvent<HTMLInputElement>): void {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      if (!open) {
        openSearchCombobox(true)
        return
      }
      if (visibleOptions.length) setHighlightedIndex((current) => Math.min(current + 1, visibleOptions.length - 1))
      return
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      if (!open) {
        openSearchCombobox(true)
        return
      }
      if (visibleOptions.length) setHighlightedIndex((current) => Math.max(current - 1, 0))
      return
    }
    if (event.key === 'Home' && open && visibleOptions.length) {
      event.preventDefault()
      setHighlightedIndex(0)
      return
    }
    if (event.key === 'End' && open && visibleOptions.length) {
      event.preventDefault()
      setHighlightedIndex(visibleOptions.length - 1)
      return
    }
    if (event.key === 'Enter' && open && visibleOptions.length) {
      event.preventDefault()
      selectSearchComboboxOption(visibleOptions[activeIndex])
      return
    }
    if (event.key === 'Escape' && open) {
      event.preventDefault()
      setOpen(false)
      setShowAllOptions(false)
    }
  }

  /** Opens the list when the text field receives focus, showing the full catalog first. */
  function handleSearchComboboxFocus(): void {
    openSearchCombobox(true)
  }

  /** Closes the menu when focus leaves the complete combobox surface. */
  function handleSearchComboboxBlur(event: FocusEvent<HTMLDivElement>): void {
    const nextTarget = event.relatedTarget
    if (!nextTarget || !event.currentTarget.contains(nextTarget)) {
      setOpen(false)
      setShowAllOptions(false)
    }
  }

  /** Selects a menu option before the input can lose focus to the option surface. */
  function handleSearchComboboxOptionPointerDown(event: ReactPointerEvent<HTMLDivElement>, option: SearchComboboxOption): void {
    event.preventDefault()
    selectSearchComboboxOption(option)
  }

  return (
    <div ref={rootRef} className={`search-combobox${open ? ' search-combobox-open' : ''}`} onBlur={handleSearchComboboxBlur}>
      <input
        ref={inputRef}
        id={id}
        role="combobox"
        aria-label={label}
        aria-autocomplete="list"
        aria-controls={listboxID}
        aria-describedby={describedBy}
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-activedescendant={open && visibleOptions.length ? `${listboxID}-option-${activeIndex}` : undefined}
        autoComplete="off"
        value={value}
        placeholder={placeholder}
        onFocus={handleSearchComboboxFocus}
        onChange={(event) => handleSearchComboboxChange(event.target.value)}
        onKeyDown={handleSearchComboboxKeyDown}
      />
      <button type="button" className="search-combobox-toggle" tabIndex={-1} aria-label={`${open ? 'Close' : 'Open'} ${label} suggestions`} onMouseDown={(event) => event.preventDefault()} onClick={toggleSearchCombobox}>
        <ChevronDown size={14} aria-hidden="true" />
      </button>
      {open ? <div id={listboxID} className="search-combobox-menu" role="listbox" aria-label={`${label} suggestions`}>
        {visibleOptions.length ? visibleOptions.map((option, index) => {
          const selected = option.value.trim().toLowerCase() === value.trim().toLowerCase()
          const active = index === activeIndex
          return <div id={`${listboxID}-option-${index}`} className={`search-combobox-option${selected ? ' search-combobox-option-selected' : ''}${active ? ' search-combobox-option-active' : ''}`} key={`${option.value}-${index}`} role="option" aria-selected={selected} onMouseEnter={() => setHighlightedIndex(index)} onPointerDown={(event) => handleSearchComboboxOptionPointerDown(event, option)}><span className="search-combobox-option-label">{option.label}</span>{option.detail ? <span className="search-combobox-option-detail">{option.detail}</span> : null}</div>
        }) : <div className="search-combobox-empty" role="status">{emptyMessage}</div>}
      </div> : null}
    </div>
  )
}
