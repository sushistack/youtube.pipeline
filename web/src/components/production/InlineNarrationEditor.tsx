import { useCallback, useEffect, useRef, useState } from 'react'
import type { UseMutationResult } from '@tanstack/react-query'
import type { Scene } from '../../contracts/runContracts'
import type { ApiClientError } from '../../lib/apiClient'

interface InlineNarrationEditorProps {
  scene: Scene
  mutation: UseMutationResult<Scene, ApiClientError, { narration: string; scene_index: number }>
  is_active: boolean
  on_activate: () => void
  on_deactivate: () => void
}

type EditState = 'edit' | 'saving' | 'error'

export function InlineNarrationEditor({
  scene,
  mutation,
  is_active,
  on_activate,
  on_deactivate,
}: InlineNarrationEditorProps) {
  // save_state is only relevant while is_active=true.
  const [save_state, set_save_state] = useState<EditState>('edit')
  // draft holds the textarea value during editing.
  const [draft, set_draft] = useState(scene.narration)
  // revert_to is the narration captured at edit-mode entry; used for Ctrl+Z and failed-save restore.
  const [revert_to, set_revert_to] = useState(scene.narration)
  const [error_msg, set_error_msg] = useState<string | null>(null)
  const textarea_ref = useRef<HTMLTextAreaElement>(null)
  // Ref guard prevents duplicate saves when Enter + blur fire before the 'saving' state settles.
  const is_saving_ref = useRef(false)

  // Autofocus textarea on edit mode entry.
  useEffect(() => {
    if (is_active) {
      textarea_ref.current?.focus()
    }
  }, [is_active])

  // enter_edit captures the current persisted narration and activates the editor.
  const enter_edit = useCallback(() => {
    set_revert_to(scene.narration)
    set_draft(scene.narration)
    set_save_state('edit')
    set_error_msg(null)
    on_activate()
  }, [scene.narration, on_activate])

  const save = useCallback(() => {
    if (is_saving_ref.current) return
    const value = draft.trim()
    if (value === revert_to.trim()) {
      on_deactivate()
      return
    }
    is_saving_ref.current = true
    set_save_state('saving')
    mutation.mutate(
      { narration: value, scene_index: scene.scene_index },
      {
        onSuccess: (saved) => {
          is_saving_ref.current = false
          set_draft(saved.narration)
          set_revert_to(saved.narration)
          set_save_state('edit')
          set_error_msg(null)
          on_deactivate()
        },
        onError: (err) => {
          is_saving_ref.current = false
          set_draft(revert_to)
          set_error_msg(err.message ?? 'Save failed. Try again.')
          set_save_state('error')
        },
      },
    )
  }, [draft, mutation, on_deactivate, revert_to, scene.scene_index])

  const handle_key_down = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault()
        save()
        return
      }
      if (e.key === 'z' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault()
        set_draft(revert_to)
        return
      }
    },
    [save, revert_to],
  )

  const handle_blur = useCallback(() => {
    save()
  }, [save])

  const handle_key_down_read = useCallback(
    (e: React.KeyboardEvent<HTMLDivElement>) => {
      if (e.key === 'Enter' || e.key === ' ' || e.key === 'Tab') {
        e.preventDefault()
        enter_edit()
      }
    },
    [enter_edit],
  )

  return (
    <div
      className="inline-narration-editor"
      data-editing={is_active ? 'true' : undefined}
      data-state={is_active ? save_state : 'read'}
    >
      <div className="inline-narration-editor__header">
        <span className="inline-narration-editor__scene-label">
          Scene {scene.scene_index + 1}
        </span>
        {is_active && save_state === 'saving' && (
          <span className="inline-narration-editor__status" aria-live="polite">
            Saving…
          </span>
        )}
      </div>

      {is_active ? (
        <textarea
          ref={textarea_ref}
          aria-label={`Narration for scene ${scene.scene_index + 1}`}
          className="inline-narration-editor__textarea"
          disabled={save_state === 'saving'}
          onBlur={handle_blur}
          onChange={(e) => set_draft(e.target.value)}
          onKeyDown={handle_key_down}
          rows={4}
          value={draft}
        />
      ) : (
        <div
          aria-label={`Narration for scene ${scene.scene_index + 1}. Press Tab, Enter, or click to edit.`}
          className="inline-narration-editor__paragraph"
          onClick={enter_edit}
          onKeyDown={handle_key_down_read}
          role="button"
          tabIndex={0}
        >
          {scene.narration || <em className="inline-narration-editor__empty">No narration</em>}
        </div>
      )}

      {is_active && save_state === 'error' && error_msg && (
        <p className="inline-narration-editor__error" role="alert">
          {error_msg}
        </p>
      )}
    </div>
  )
}
