/* eslint-disable react-refresh/only-export-components */
import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from 'react'
import {
  type ShortcutDefinition,
  type ShortcutHandlerContext,
  type ShortcutKey,
  type ShortcutScope,
  isEditableEventTarget,
  normalizeShortcut,
} from '../lib/keyboardShortcuts'

interface RegisteredShortcut extends ShortcutDefinition {
  order: number
  scope: ShortcutScope
}

type ShortcutSupplier = () => ShortcutDefinition[]

interface SupplierEntry {
  order: number
  supplier: ShortcutSupplier
}

interface KeyboardShortcutsRegistry {
  register: (supplier: ShortcutSupplier) => () => void
}

const NESTED_PROVIDER_FLAG = Symbol.for('youtube-pipeline/keyboard-provider')
const KeyboardShortcutsContext = createContext<KeyboardShortcutsRegistry | null>(null)

function compareRegisteredShortcuts(
  left: RegisteredShortcut,
  right: RegisteredShortcut,
) {
  if (left.scope !== right.scope) {
    return left.scope === 'context' ? -1 : 1
  }
  return right.order - left.order
}

function buildHandlerContext(
  event: KeyboardEvent,
  key: ShortcutKey,
  digit: number | null,
): ShortcutHandlerContext {
  return {
    digit,
    key,
    original_event: event,
    target: event.target,
  }
}

function warnNestedProvider() {
  if (typeof window === 'undefined') {
    return () => {}
  }

  const globalTarget = window as unknown as Record<symbol, number | undefined>
  const current = globalTarget[NESTED_PROVIDER_FLAG] ?? 0
  globalTarget[NESTED_PROVIDER_FLAG] = current + 1

  if (current >= 1 && import.meta.env?.MODE !== 'test') {
    console.warn(
      '[KeyboardShortcutsProvider] Multiple providers detected — keyboard events will be dispatched by each provider independently. Nest a single provider at the application root.',
    )
  }

  return () => {
    const next = (globalTarget[NESTED_PROVIDER_FLAG] ?? 1) - 1
    if (next <= 0) {
      delete globalTarget[NESTED_PROVIDER_FLAG]
    } else {
      globalTarget[NESTED_PROVIDER_FLAG] = next
    }
  }
}

export function KeyboardShortcutsProvider({
  children,
}: {
  children: ReactNode
}) {
  const suppliers_ref = useRef(new Set<SupplierEntry>())
  const next_order_ref = useRef(0)

  useEffect(() => {
    const cleanup = warnNestedProvider()
    return cleanup
  }, [])

  useEffect(() => {
    function collectShortcuts(): RegisteredShortcut[] {
      const collected: RegisteredShortcut[] = []
      for (const entry of suppliers_ref.current) {
        const definitions = entry.supplier()
        for (const definition of definitions) {
          collected.push({
            ...definition,
            order: entry.order,
            scope: definition.scope ?? 'global',
          })
        }
      }
      return collected
    }

    function on_key_down(event: KeyboardEvent) {
      const normalized = normalizeShortcut(event)
      if (!normalized) {
        return
      }

      const editable_target = isEditableEventTarget(event.target, event)
      const matched = collectShortcuts()
        .filter((shortcut) => {
          if (shortcut.key !== normalized.key || shortcut.enabled === false) {
            return false
          }
          if (editable_target && shortcut.allow_in_editable !== true) {
            return false
          }
          return true
        })
        .sort(compareRegisteredShortcuts)[0]

      if (!matched) {
        return
      }

      if (matched.prevent_default) {
        event.preventDefault()
      }

      try {
        matched.handler(
          buildHandlerContext(event, normalized.key, normalized.digit),
        )
      } catch (error) {
        console.error('[KeyboardShortcutsProvider] handler threw', error)
      }
    }

    window.addEventListener('keydown', on_key_down)
    return () => {
      window.removeEventListener('keydown', on_key_down)
    }
  }, [])

  const [registry] = useState<KeyboardShortcutsRegistry>(() => ({
    register(supplier) {
      const entry: SupplierEntry = {
        order: next_order_ref.current,
        supplier,
      }
      next_order_ref.current += 1
      suppliers_ref.current.add(entry)

      return () => {
        suppliers_ref.current.delete(entry)
      }
    },
  }))

  return (
    <KeyboardShortcutsContext.Provider value={registry}>
      {children}
    </KeyboardShortcutsContext.Provider>
  )
}

export function useKeyboardShortcuts(
  shortcuts: ShortcutDefinition[],
  options?: {
    enabled?: boolean
  },
) {
  const registry = useContext(KeyboardShortcutsContext)

  if (!registry) {
    throw new Error(
      'useKeyboardShortcuts must be used within a KeyboardShortcutsProvider',
    )
  }

  const shortcuts_ref = useRef(shortcuts)
  useLayoutEffect(() => {
    shortcuts_ref.current = shortcuts
  })

  const enabled = options?.enabled !== false

  useEffect(() => {
    if (!enabled) {
      return
    }
    return registry.register(() => shortcuts_ref.current)
  }, [enabled, registry])
}
