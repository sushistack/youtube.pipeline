import { describe, expect, it } from 'vitest'
import {
  formatShortcutHint,
  isEditableEventTarget,
  normalizeShortcut,
} from './keyboardShortcuts'

function createKeyboardEvent(
  key: string,
  options: KeyboardEventInit = {},
) {
  return new KeyboardEvent('keydown', {
    bubbles: true,
    key,
    ...options,
  })
}

describe('normalizeShortcut — supported combinations', () => {
  it('normalizes the required key set centrally', () => {
    expect(normalizeShortcut(createKeyboardEvent('Enter'))).toEqual({
      digit: null,
      key: 'enter',
    })
    expect(
      normalizeShortcut(createKeyboardEvent('Enter', { shiftKey: true })),
    ).toEqual({
      digit: null,
      key: 'shift+enter',
    })
    expect(normalizeShortcut(createKeyboardEvent('Escape'))).toEqual({
      digit: null,
      key: 'escape',
    })
    expect(normalizeShortcut(createKeyboardEvent('Tab'))).toEqual({
      digit: null,
      key: 'tab',
    })
    expect(
      normalizeShortcut(createKeyboardEvent('z', { ctrlKey: true })),
    ).toEqual({
      digit: null,
      key: 'ctrl+z',
    })
    for (let digit = 0; digit <= 9; digit += 1) {
      expect(normalizeShortcut(createKeyboardEvent(String(digit)))).toEqual({
        digit,
        key: `digit-${digit}`,
      })
    }
  })

  it('normalizes lowercase letter shortcuts without modifiers', () => {
    expect(normalizeShortcut(createKeyboardEvent('j'))).toEqual({
      digit: null,
      key: 'j',
    })
    expect(normalizeShortcut(createKeyboardEvent('K'))).toEqual({
      digit: null,
      key: 'k',
    })
  })

  it('normalizes mod+n on macOS and non-macOS platforms', () => {
    const original_platform = navigator.platform

    Object.defineProperty(window.navigator, 'platform', {
      configurable: true,
      value: 'MacIntel',
    })
    expect(
      normalizeShortcut(createKeyboardEvent('n', { metaKey: true })),
    ).toEqual({
      digit: null,
      key: 'mod+n',
    })
    expect(formatShortcutHint('mod+n')).toBe('⌘N')

    Object.defineProperty(window.navigator, 'platform', {
      configurable: true,
      value: 'Win32',
    })
    expect(
      normalizeShortcut(createKeyboardEvent('n', { ctrlKey: true })),
    ).toEqual({
      digit: null,
      key: 'mod+n',
    })
    expect(formatShortcutHint('mod+n')).toBe('Ctrl+N')

    Object.defineProperty(window.navigator, 'platform', {
      configurable: true,
      value: original_platform,
    })
  })
})

describe('normalizeShortcut — negative guards', () => {
  it('returns null for plain z without ctrl', () => {
    expect(normalizeShortcut(createKeyboardEvent('z'))).toBeNull()
  })

  it('returns null for Cmd+Z (metaKey) because Ctrl+Z is required', () => {
    expect(
      normalizeShortcut(createKeyboardEvent('z', { metaKey: true })),
    ).toBeNull()
  })

  it('returns null for Ctrl+Shift+Z so redo does not collapse into undo', () => {
    expect(
      normalizeShortcut(
        createKeyboardEvent('z', { ctrlKey: true, shiftKey: true }),
      ),
    ).toBeNull()
  })

  it('returns null while an IME is composing', () => {
    expect(
      normalizeShortcut(createKeyboardEvent('Enter', { isComposing: true })),
    ).toBeNull()
  })

  it('returns null for auto-repeat events', () => {
    expect(
      normalizeShortcut(createKeyboardEvent('Enter', { repeat: true })),
    ).toBeNull()
  })

  it('returns null for Shift+letter so it does not collide with bare letters', () => {
    expect(
      normalizeShortcut(createKeyboardEvent('S', { shiftKey: true })),
    ).toBeNull()
    expect(
      normalizeShortcut(createKeyboardEvent('J', { shiftKey: true })),
    ).toBeNull()
    expect(
      normalizeShortcut(createKeyboardEvent('K', { shiftKey: true })),
    ).toBeNull()
  })

  it('returns null for Shift+Tab and Ctrl+Tab', () => {
    expect(
      normalizeShortcut(createKeyboardEvent('Tab', { shiftKey: true })),
    ).toBeNull()
    expect(
      normalizeShortcut(createKeyboardEvent('Tab', { ctrlKey: true })),
    ).toBeNull()
  })

  it('returns null for Ctrl+Enter and Alt+Enter', () => {
    expect(
      normalizeShortcut(createKeyboardEvent('Enter', { ctrlKey: true })),
    ).toBeNull()
    expect(
      normalizeShortcut(createKeyboardEvent('Enter', { altKey: true })),
    ).toBeNull()
  })

  it('returns null for Shift+digit', () => {
    expect(
      normalizeShortcut(createKeyboardEvent('1', { shiftKey: true })),
    ).toBeNull()
  })

  it('returns null for plain n, Shift+N, and Alt+N', () => {
    expect(normalizeShortcut(createKeyboardEvent('n'))).toBeNull()
    expect(
      normalizeShortcut(createKeyboardEvent('N', { shiftKey: true })),
    ).toBeNull()
    expect(
      normalizeShortcut(createKeyboardEvent('n', { altKey: true })),
    ).toBeNull()
  })
})

describe('isEditableEventTarget', () => {
  it('classifies text-like inputs as editable', () => {
    const input = document.createElement('input')
    const textarea = document.createElement('textarea')
    const editable = document.createElement('div')
    editable.setAttribute('contenteditable', 'true')

    expect(isEditableEventTarget(input)).toBe(true)
    expect(isEditableEventTarget(textarea)).toBe(true)
    expect(isEditableEventTarget(editable)).toBe(true)
  })

  it('does not classify buttons or non-text controls as editable', () => {
    const button = document.createElement('button')
    const checkbox = document.createElement('input')
    checkbox.type = 'checkbox'
    const radio = document.createElement('input')
    radio.type = 'radio'
    const submit = document.createElement('input')
    submit.type = 'submit'

    expect(isEditableEventTarget(button)).toBe(false)
    expect(isEditableEventTarget(checkbox)).toBe(false)
    expect(isEditableEventTarget(radio)).toBe(false)
    expect(isEditableEventTarget(submit)).toBe(false)
  })

  it('excludes readOnly and disabled text inputs from editable-suppression', () => {
    const read_only = document.createElement('input')
    read_only.type = 'text'
    read_only.readOnly = true

    const disabled_textarea = document.createElement('textarea')
    disabled_textarea.disabled = true

    expect(isEditableEventTarget(read_only)).toBe(false)
    expect(isEditableEventTarget(disabled_textarea)).toBe(false)
  })

  it('walks ancestors to detect contenteditable containers', () => {
    const container = document.createElement('div')
    container.setAttribute('contenteditable', 'true')
    const inner = document.createElement('span')
    container.appendChild(inner)
    document.body.appendChild(container)

    try {
      expect(isEditableEventTarget(inner)).toBe(true)
    } finally {
      document.body.removeChild(container)
    }
  })

  it('ignores contenteditable="false" ancestors', () => {
    const container = document.createElement('div')
    container.setAttribute('contenteditable', 'false')
    const inner = document.createElement('span')
    container.appendChild(inner)
    document.body.appendChild(container)

    try {
      expect(isEditableEventTarget(inner)).toBe(false)
    } finally {
      document.body.removeChild(container)
    }
  })
})
