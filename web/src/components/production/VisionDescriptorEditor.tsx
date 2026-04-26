import { useCallback, useEffect, useRef, useState } from 'react'

interface VisionDescriptorEditorProps {
  prefill: string
  onDescriptorChange: (v: string) => void
  onConfirm: () => void
  is_submitting?: boolean
}

export function VisionDescriptorEditor({
  prefill,
  onDescriptorChange,
  onConfirm,
  is_submitting = false,
}: VisionDescriptorEditorProps) {
  const [draft, set_draft] = useState(prefill)
  const [revert_to, set_revert_to] = useState(prefill)
  const [last_prefill, set_last_prefill] = useState(prefill)
  const [edit_mode, set_edit_mode] = useState(false)
  const textarea_ref = useRef<HTMLTextAreaElement>(null)

  // Detect prefill prop change during render and reset draft + revert target.
  // This is the React-recommended pattern for syncing derived state to a prop
  // without the overhead (and lint friction) of an effect-scheduled setState.
  //
  // While the operator is actively editing (edit_mode=true), a background
  // refetch of /characters/descriptor that updates the prefill prop MUST NOT
  // clobber the in-progress draft — the user would silently lose their edits.
  // We still track last_prefill so the next post-edit render can catch up
  // (preventing a future stale-prefill render from comparing against a value
  // the operator never saw).
  if (last_prefill !== prefill) {
    set_last_prefill(prefill)
    if (!edit_mode) {
      set_draft(prefill)
      set_revert_to(prefill)
    }
  }

  useEffect(() => {
    if (edit_mode) {
      textarea_ref.current?.focus()
    }
  }, [edit_mode])

  const handle_read_key_down = useCallback(
    (e: React.KeyboardEvent<HTMLDivElement>) => {
      if (e.key === 'Tab') {
        e.preventDefault()
        set_edit_mode(true)
        return
      }
      if (e.key === 'Enter') {
        e.preventDefault()
        onConfirm()
      }
    },
    [onConfirm],
  )

  const handle_textarea_key_down = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'z' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault()
        set_draft(revert_to)
        // Sync parent immediately so a follow-up confirm (Ctrl+Z → blur →
        // Enter, or a programmatic focus change that skips blur) cannot
        // confirm with the pre-revert value. Without this, the parent's
        // descriptor ref only updates on blur — opening a stale-closure
        // window where the on-screen textarea shows revert_to but the
        // parent ref still holds the edited value.
        onDescriptorChange(revert_to.trim())
      }
    },
    [onDescriptorChange, revert_to],
  )

  const handle_blur = useCallback(() => {
    set_edit_mode(false)
    onDescriptorChange(draft.trim())
  }, [draft, onDescriptorChange])

  const handle_edit_toggle = useCallback(() => {
    if (edit_mode) {
      onDescriptorChange(draft.trim())
      set_edit_mode(false)
    } else {
      set_edit_mode(true)
    }
  }, [draft, edit_mode, onDescriptorChange])

  return (
    <section
      aria-label="Vision Descriptor editor"
      className="vision-descriptor"
      data-editing={edit_mode ? 'true' : undefined}
    >
      <header className="vision-descriptor__header">
        <p className="production-dashboard__eyebrow">Vision Descriptor</p>
        <h3 className="production-dashboard__section-title">
          Frozen descriptor for every shot
        </h3>
      </header>

      {edit_mode ? (
        <textarea
          aria-label="Vision Descriptor draft"
          className="vision-descriptor__textarea"
          disabled={is_submitting}
          onBlur={handle_blur}
          onChange={(e) => set_draft(e.target.value)}
          onKeyDown={handle_textarea_key_down}
          ref={textarea_ref}
          rows={6}
          value={draft}
        />
      ) : (
        <div
          aria-label="Vision Descriptor draft. Press Tab to edit or Enter to confirm."
          className="vision-descriptor__paragraph"
          onKeyDown={handle_read_key_down}
          role="button"
          tabIndex={0}
        >
          {draft || <em className="vision-descriptor__empty">No descriptor</em>}
        </div>
      )}

      <footer className="vision-descriptor__actions">
        <button
          className="vision-descriptor__secondary"
          disabled={is_submitting}
          onClick={handle_edit_toggle}
          // Keep textarea focused during the click so the natural blur path
          // (handle_blur → set_edit_mode(false)) cannot race with this
          // onClick's stale closure and silently re-enter edit mode.
          onMouseDown={(e) => {
            if (edit_mode) e.preventDefault()
          }}
          type="button"
        >
          {edit_mode ? 'Save edit' : 'Edit descriptor'}
        </button>
        <p className="vision-descriptor__hint vision-descriptor__hint--inline">
          Tab edit · Ctrl+Z revert · Enter confirm
        </p>
        <button
          className="vision-descriptor__primary"
          disabled={is_submitting}
          onClick={onConfirm}
          type="button"
        >
          {is_submitting ? 'Saving…' : 'Confirm & continue'}
        </button>
      </footer>
    </section>
  )
}
