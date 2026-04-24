import { useState } from 'react'
import type { CriticPromptEnvelope } from '../../contracts/tuningContracts'
import { ApiClientError } from '../../lib/apiClient'
import {
  useCriticPromptMutation,
  useCriticPromptQuery,
} from '../../hooks/useTuning'

export interface CriticPromptSectionProps {
  onSaved?: (envelope: CriticPromptEnvelope) => void
}

// draft===null means "clean, tracking the server copy"; a non-null draft is
// the operator's unsaved edit. handleSave resets back to null on success so
// the next render reads from query.data again.
export function CriticPromptSection({ onSaved }: CriticPromptSectionProps) {
  const query = useCriticPromptQuery()
  const mutation = useCriticPromptMutation()

  const [draft, setDraft] = useState<string | null>(null)
  const [status, setStatus] = useState<string | null>(null)

  if (query.isPending) {
    return (
      <section
        className="tuning-section"
        aria-labelledby="tuning-critic-prompt-heading"
      >
        <h2 id="tuning-critic-prompt-heading" className="tuning-section__title">
          Critic Prompt
        </h2>
        <p className="tuning-section__body">Loading prompt…</p>
      </section>
    )
  }

  if (query.isError || !query.data) {
    const message =
      query.error instanceof ApiClientError
        ? query.error.message
        : 'Failed to load prompt.'
    return (
      <section
        className="tuning-section"
        aria-labelledby="tuning-critic-prompt-heading"
      >
        <h2 id="tuning-critic-prompt-heading" className="tuning-section__title">
          Critic Prompt
        </h2>
        <p className="tuning-section__error" role="alert">
          {message}
        </p>
      </section>
    )
  }

  const envelope = query.data
  const value = draft ?? envelope.body
  const isDirty = draft !== null && draft !== envelope.body

  async function handleSave() {
    if (!isDirty || draft === null) {
      return
    }
    setStatus(null)
    try {
      const saved = await mutation.mutateAsync(draft)
      setDraft(null)
      setStatus(`Saved as ${saved.version_tag}.`)
      onSaved?.(saved)
    } catch (error) {
      setStatus(
        error instanceof Error ? error.message : 'Failed to save prompt.',
      )
    }
  }

  return (
    <section
      className="tuning-section"
      aria-labelledby="tuning-critic-prompt-heading"
    >
      <div className="tuning-section__header">
        <h2
          id="tuning-critic-prompt-heading"
          className="tuning-section__title"
        >
          Critic Prompt
        </h2>
        <p className="tuning-section__meta">
          hash <code>{envelope.prompt_hash.slice(0, 12)}…</code>
          {envelope.version_tag ? (
            <>
              {' '}· version <code>{envelope.version_tag}</code>
            </>
          ) : null}
        </p>
      </div>
      <textarea
        className="tuning-prompt__editor"
        aria-label="Critic prompt body"
        value={value}
        rows={18}
        onChange={(event) => setDraft(event.target.value)}
      />
      <div className="tuning-section__actions">
        <button
          type="button"
          className="tuning-button tuning-button--primary"
          disabled={!isDirty || mutation.isPending}
          onClick={handleSave}
        >
          {mutation.isPending ? 'Saving…' : isDirty ? 'Save prompt' : 'Saved'}
        </button>
        {status ? (
          <p className="tuning-section__status" role="status">
            {status}
          </p>
        ) : null}
      </div>
    </section>
  )
}
