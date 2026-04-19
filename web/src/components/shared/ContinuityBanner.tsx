import { useEffect, useRef } from 'react'

interface ContinuityBannerProps {
  message: string
  on_dismiss: () => void
}

function isEditableTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) {
    return false
  }

  const tag = target.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') {
    return true
  }

  return target.isContentEditable
}

export function ContinuityBanner({
  message,
  on_dismiss,
}: ContinuityBannerProps) {
  const on_dismiss_ref = useRef(on_dismiss)

  useEffect(() => {
    on_dismiss_ref.current = on_dismiss
  })

  useEffect(() => {
    const timeout_id = window.setTimeout(() => {
      on_dismiss_ref.current()
    }, 5000)

    function dismissFromInteraction(event: Event) {
      if (isEditableTarget(event.target)) {
        return
      }

      if (event instanceof KeyboardEvent && event.isComposing) {
        return
      }

      window.clearTimeout(timeout_id)
      window.removeEventListener('pointerdown', dismissFromInteraction)
      window.removeEventListener('keydown', dismissFromInteraction)
      on_dismiss_ref.current()
    }

    window.addEventListener('pointerdown', dismissFromInteraction)
    window.addEventListener('keydown', dismissFromInteraction)

    return () => {
      window.clearTimeout(timeout_id)
      window.removeEventListener('pointerdown', dismissFromInteraction)
      window.removeEventListener('keydown', dismissFromInteraction)
    }
  }, [message])

  return (
    <section className="continuity-banner" role="status" aria-live="polite">
      <p className="continuity-banner__eyebrow">What changed since last session</p>
      <p className="continuity-banner__message">{message}</p>
    </section>
  )
}
