import { useEffect, useRef, useState } from 'react'
import type { PriorRejectionWarning } from '../../contracts/runContracts'

interface RejectComposerProps {
  is_submitting: boolean
  on_cancel: () => void
  on_submit: (reason: string) => void
  prior_rejection?: PriorRejectionWarning | null
  scene_index: number
}

function formatPriorTimestamp(raw: string) {
  try {
    return new Date(raw).toLocaleString()
  } catch {
    return raw
  }
}

export function RejectComposer({
  is_submitting,
  on_cancel,
  on_submit,
  prior_rejection = null,
  scene_index,
}: RejectComposerProps) {
  const [reason, set_reason] = useState('')
  const [validation_error, set_validation_error] = useState<string | null>(null)
  const textarea_ref = useRef<HTMLTextAreaElement | null>(null)

  useEffect(() => {
    textarea_ref.current?.focus()
  }, [])

  function submit() {
    const trimmed = reason.trim()
    if (trimmed === '') {
      set_validation_error('A rejection reason is required.')
      return
    }
    set_validation_error(null)
    on_submit(trimmed)
  }

  // Attached at the <section> root so Esc still cancels an empty composer
  // when focus has moved to Confirm / Cancel buttons or the FR53 warning —
  // the global keyboard-shortcut hook is suppressed while the composer is
  // open, so without this root handler Esc would simply be swallowed.
  function handleKeyDown(event: React.KeyboardEvent<HTMLElement>) {
    if (event.key === 'Escape') {
      if (reason.trim() === '') {
        event.preventDefault()
        event.stopPropagation()
        on_cancel()
      }
      // Non-empty: preserve draft. Native Esc is a no-op inside a textarea,
      // so we simply let the keydown fall through.
      return
    }
    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
      event.preventDefault()
      event.stopPropagation()
      submit()
    }
  }

  return (
    <section
      className="reject-composer"
      aria-label={`Reject composer for scene ${scene_index + 1}`}
      // role="region" is implied by <section aria-label>. We explicitly do
      // NOT use role="dialog" — Story 8.4 AC-1 keeps this composer inline.
      onKeyDown={handleKeyDown}
    >
      <header className="reject-composer__header">
        <h3 className="reject-composer__title">Why are you rejecting scene {scene_index + 1}?</h3>
        <p className="reject-composer__hint">
          Ctrl/⌘+Enter to submit · Esc to cancel when empty
        </p>
      </header>

      {prior_rejection ? (
        <div
          className="reject-composer__fr53-warning"
          role="note"
          aria-label="Prior rejection warning"
        >
          <strong className="reject-composer__fr53-title">We&apos;ve seen this scene fail before.</strong>
          <p className="reject-composer__fr53-body">
            Run <code>{prior_rejection.run_id}</code> rejected scene{' '}
            {prior_rejection.scene_index + 1} on{' '}
            {formatPriorTimestamp(prior_rejection.created_at)}:
          </p>
          <blockquote className="reject-composer__fr53-reason">{prior_rejection.reason}</blockquote>
        </div>
      ) : null}

      <label className="reject-composer__label">
        <span className="reject-composer__label-text">Rejection reason</span>
        <textarea
          ref={textarea_ref}
          className="reject-composer__input"
          aria-label="Rejection reason"
          aria-invalid={validation_error ? true : undefined}
          rows={3}
          value={reason}
          disabled={is_submitting}
          onChange={(event) => {
            set_reason(event.target.value)
            if (validation_error) {
              set_validation_error(null)
            }
          }}
        />
      </label>

      {validation_error ? (
        <p className="reject-composer__error" role="alert">
          {validation_error}
        </p>
      ) : null}

      <div className="reject-composer__actions">
        <button
          type="button"
          className="reject-composer__button reject-composer__button--primary"
          disabled={is_submitting}
          onClick={submit}
        >
          {is_submitting ? 'Rejecting…' : 'Confirm reject'}
        </button>
        <button
          type="button"
          className="reject-composer__button"
          disabled={is_submitting}
          onClick={on_cancel}
        >
          Cancel
        </button>
      </div>
    </section>
  )
}
