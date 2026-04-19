import { useSyncExternalStore } from 'react'

const SIDEBAR_COLLAPSE_QUERY = '(width < 1280px)'

function get_matches() {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return false
  }

  try {
    return window.matchMedia(SIDEBAR_COLLAPSE_QUERY).matches
  } catch {
    return false
  }
}

function subscribe(on_store_change: () => void) {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return () => undefined
  }

  let media_query: MediaQueryList
  try {
    media_query = window.matchMedia(SIDEBAR_COLLAPSE_QUERY)
  } catch {
    return () => undefined
  }

  media_query.addEventListener('change', on_store_change)

  return () => {
    media_query.removeEventListener('change', on_store_change)
  }
}

export function useViewportCollapse() {
  return useSyncExternalStore(subscribe, get_matches, () => false)
}
