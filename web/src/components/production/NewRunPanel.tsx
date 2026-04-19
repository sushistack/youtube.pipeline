import {
  useEffect,
  useId,
  useRef,
  useState,
  type KeyboardEvent,
} from 'react'
import { createRun, ApiClientError } from '../../lib/apiClient'
import {
  SCP_ID_PATTERN,
  type RunDetail,
} from '../../contracts/runContracts'

interface NewRunPanelProps {
  on_cancel: () => void
  on_success: (run: RunDetail) => void | Promise<void>
}

function focusableElements(root: HTMLElement | null) {
  if (!root) {
    return []
  }

  return Array.from(
    root.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  )
}

function getValidationError(raw_value: string) {
  const trimmed = raw_value.trim()
  if (trimmed.length === 0) {
    return null
  }

  if (!SCP_ID_PATTERN.test(trimmed)) {
    return 'SCP ID must be alphanumeric, hyphen, or underscore'
  }

  return null
}

function getSubmissionError(error: unknown) {
  if (error instanceof ApiClientError) {
    if (error.status === 400) {
      return `The server rejected that SCP ID: ${error.message}. Check the format and try again.`
    }

    return `Server error (${error.status}). The run was not created. Retry, or check the server logs.`
  }

  return "Couldn't reach the server. Check that `pipeline serve` is running, then retry."
}

export function NewRunPanel({
  on_cancel,
  on_success,
}: NewRunPanelProps) {
  const panel_ref = useRef<HTMLDivElement | null>(null)
  const input_ref = useRef<HTMLInputElement | null>(null)
  const is_mounted_ref = useRef(true)
  const [scp_id, set_scp_id] = useState('')
  const [server_error, set_server_error] = useState<string | null>(null)
  const [is_submitting, set_is_submitting] = useState(false)
  const instance_id = useId()
  const title_id = `new-run-title-${instance_id}`
  const description_id = `new-run-copy-${instance_id}`
  const error_id = `new-run-error-${instance_id}`
  const validation_error = getValidationError(scp_id)
  const trimmed_scp_id = scp_id.trim()
  const submit_disabled =
    is_submitting || trimmed_scp_id.length === 0 || validation_error != null

  useEffect(() => {
    input_ref.current?.focus()
    return () => {
      is_mounted_ref.current = false
    }
  }, [])

  function dismiss() {
    if (is_submitting) {
      return
    }
    on_cancel()
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.nativeEvent.stopImmediatePropagation()
      dismiss()
      return
    }

    if (event.key === 'Enter') {
      const active = document.activeElement
      if (
        active instanceof HTMLButtonElement &&
        panel_ref.current?.contains(active)
      ) {
        event.preventDefault()
        active.click()
        return
      }

      if (
        active instanceof HTMLInputElement &&
        panel_ref.current?.contains(active)
      ) {
        event.preventDefault()
        void handleSubmit()
      }
      return
    }

    if (event.key !== 'Tab') {
      return
    }

    const targets = focusableElements(panel_ref.current)
    if (targets.length === 0) {
      event.preventDefault()
      panel_ref.current?.focus()
      return
    }

    const active = document.activeElement as HTMLElement | null
    const current_index = active ? targets.indexOf(active) : -1
    const next_index = event.shiftKey
      ? current_index <= 0
        ? targets.length - 1
        : current_index - 1
      : current_index === -1 || current_index === targets.length - 1
        ? 0
        : current_index + 1

    event.preventDefault()
    targets[next_index]?.focus()
  }

  async function handleSubmit() {
    if (submit_disabled) {
      return
    }

    set_is_submitting(true)
    set_server_error(null)

    try {
      const run = await createRun(trimmed_scp_id)
      await on_success(run)
    } catch (error) {
      if (is_mounted_ref.current) {
        set_server_error(getSubmissionError(error))
        input_ref.current?.focus()
      }
    } finally {
      if (is_mounted_ref.current) {
        set_is_submitting(false)
      }
    }
  }

  return (
    <div
      ref={panel_ref}
      aria-describedby={description_id}
      aria-labelledby={title_id}
      className="new-run-panel"
      onKeyDown={handleKeyDown}
      role="alertdialog"
      tabIndex={-1}
    >
      <p className="new-run-panel__eyebrow">Production workflow</p>
      <h3 className="new-run-panel__title" id={title_id}>
        Create a new pipeline run
      </h3>
      <p className="new-run-panel__copy" id={description_id}>
        Enter an SCP ID. Alphanumeric, hyphen, or underscore. Example: `049`
      </p>

      <label className="new-run-panel__field">
        <span className="new-run-panel__label">SCP ID</span>
        <input
          ref={input_ref}
          aria-describedby={
            validation_error || server_error
              ? `${description_id} ${error_id}`
              : description_id
          }
          aria-invalid={validation_error != null || server_error != null}
          className="new-run-panel__input"
          onChange={(event) => {
            set_scp_id(event.target.value)
            set_server_error(null)
          }}
          placeholder="049"
          readOnly={is_submitting}
          type="text"
          value={scp_id}
        />
      </label>

      {validation_error || server_error ? (
        <p className="new-run-panel__error" id={error_id} role="alert">
          {validation_error ?? server_error}
        </p>
      ) : null}

      <div className="new-run-panel__actions">
        <button
          type="button"
          className="sidebar__new-run-submit"
          disabled={submit_disabled}
          onClick={() => {
            void handleSubmit()
          }}
        >
          {is_submitting ? 'Creating…' : 'Create'}
        </button>
        <button
          type="button"
          className="sidebar__new-run-cancel"
          disabled={is_submitting}
          onClick={dismiss}
        >
          Cancel
        </button>
      </div>
    </div>
  )
}
