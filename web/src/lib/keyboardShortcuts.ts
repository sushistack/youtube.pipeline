export const SUPPORTED_SHORTCUT_KEYS = [
  'enter',
  'shift+enter',
  'escape',
  'tab',
  'ctrl+z',
  'space',
  's',
  'j',
  'k',
  'digit-1',
  'digit-2',
  'digit-3',
  'digit-4',
  'digit-5',
  'digit-6',
  'digit-7',
  'digit-8',
  'digit-9',
  'digit-0',
] as const

export type ShortcutKey = (typeof SUPPORTED_SHORTCUT_KEYS)[number]
export type ShortcutScope = 'global' | 'context'

export interface NormalizedShortcut {
  digit: number | null
  key: ShortcutKey
}

export interface ShortcutHandlerContext {
  digit: number | null
  key: ShortcutKey
  original_event: KeyboardEvent
  target: EventTarget | null
}

export interface ShortcutDefinition {
  action?: string
  allow_in_editable?: boolean
  enabled?: boolean
  handler: (context: ShortcutHandlerContext) => void
  key: ShortcutKey
  prevent_default?: boolean
  scope?: ShortcutScope
}

const LETTER_SHORTCUTS = new Set<ShortcutKey>(['s', 'j', 'k'])
const DIGIT_SHORTCUTS = new Set<ShortcutKey>(
  SUPPORTED_SHORTCUT_KEYS.filter((key) => key.startsWith('digit-')),
)

const EDITABLE_INPUT_TYPES = new Set([
  'date',
  'datetime-local',
  'email',
  'month',
  'number',
  'password',
  'search',
  'tel',
  'text',
  'time',
  'url',
  'week',
])

export function formatShortcutHint(key: ShortcutKey) {
  return key
}

export function normalizeShortcut(event: KeyboardEvent): NormalizedShortcut | null {
  if (event.isComposing || event.keyCode === 229) {
    return null
  }

  if (event.repeat) {
    return null
  }

  const key = event.key
  const lowered = key.toLowerCase()

  if (lowered === 'enter') {
    if (event.ctrlKey || event.altKey || event.metaKey) {
      return null
    }
    return {
      digit: null,
      key: event.shiftKey ? 'shift+enter' : 'enter',
    }
  }

  if (lowered === 'escape' || lowered === 'esc') {
    if (event.ctrlKey || event.altKey || event.metaKey || event.shiftKey) {
      return null
    }
    return {
      digit: null,
      key: 'escape',
    }
  }

  if (lowered === 'tab') {
    if (event.ctrlKey || event.altKey || event.metaKey || event.shiftKey) {
      return null
    }
    return {
      digit: null,
      key: 'tab',
    }
  }

  if ((key === ' ' || lowered === 'spacebar') && !event.ctrlKey && !event.altKey && !event.metaKey && !event.shiftKey) {
    return {
      digit: null,
      key: 'space',
    }
  }

  if (
    event.ctrlKey &&
    !event.altKey &&
    !event.metaKey &&
    !event.shiftKey &&
    lowered === 'z'
  ) {
    return {
      digit: null,
      key: 'ctrl+z',
    }
  }

  if (!event.ctrlKey && !event.altKey && !event.metaKey && !event.shiftKey) {
    if (lowered === 's' || lowered === 'j' || lowered === 'k') {
      return {
        digit: null,
        key: lowered,
      }
    }

    if (/^[0-9]$/.test(key)) {
      return {
        digit: Number(key),
        key: `digit-${key}` as ShortcutKey,
      }
    }
  }

  return null
}

function resolveEventTarget(target: EventTarget | null, event?: Event): EventTarget | null {
  if (!event || typeof event.composedPath !== 'function') {
    return target
  }

  const path = event.composedPath()
  for (const node of path) {
    if (node instanceof HTMLElement) {
      return node
    }
  }

  return target
}

export function isEditableEventTarget(
  target: EventTarget | null,
  event?: Event,
): boolean {
  const resolved = resolveEventTarget(target, event)

  if (!(resolved instanceof HTMLElement)) {
    return false
  }

  if (resolved instanceof HTMLTextAreaElement) {
    return !resolved.readOnly && !resolved.disabled
  }

  if (resolved instanceof HTMLInputElement) {
    const type = (resolved.type || 'text').toLowerCase()
    if (!EDITABLE_INPUT_TYPES.has(type)) {
      return false
    }
    return !resolved.readOnly && !resolved.disabled
  }

  if (
    resolved.isContentEditable ||
    resolved.getAttribute('contenteditable') === '' ||
    resolved.getAttribute('contenteditable') === 'true'
  ) {
    return true
  }

  const editable_ancestor = resolved.closest<HTMLElement>(
    '[contenteditable]:not([contenteditable="false"])',
  )

  return editable_ancestor !== null
}

export function isLetterShortcut(key: ShortcutKey) {
  return LETTER_SHORTCUTS.has(key)
}

export function isDigitShortcut(key: ShortcutKey) {
  return DIGIT_SHORTCUTS.has(key)
}
