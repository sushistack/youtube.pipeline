import { useEffect, useRef } from 'react'

interface OnboardingModalProps {
  on_dismiss: () => void
}

export function OnboardingModal({ on_dismiss }: OnboardingModalProps) {
  const dismiss_button_ref = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    dismiss_button_ref.current?.focus()

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key !== 'Escape') {
        return
      }

      if (event.isComposing) {
        return
      }

      event.preventDefault()
      on_dismiss()
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [on_dismiss])

  return (
    <div className="onboarding-modal__backdrop">
      <section
        aria-labelledby="onboarding-modal-title"
        aria-modal="true"
        className="onboarding-modal"
        role="dialog"
      >
        <div className="onboarding-modal__header">
          <p className="onboarding-modal__eyebrow">Workflow overview</p>
          <h2 className="onboarding-modal__title" id="onboarding-modal-title">
            Know where to work next
          </h2>
          <p className="onboarding-modal__body">
            Production is for active run monitoring and HITL decisions. Tuning is
            for prompt, rubric, and diagnostic review. Settings is for provider
            and configuration management.
          </p>
        </div>

        <dl className="onboarding-modal__list">
          <div className="onboarding-modal__item">
            <dt>Production</dt>
            <dd>Track active runs and handle review decisions.</dd>
          </div>
          <div className="onboarding-modal__item">
            <dt>Tuning</dt>
            <dd>Inspect prompts, rubric outcomes, and diagnostics.</dd>
          </div>
          <div className="onboarding-modal__item">
            <dt>Settings</dt>
            <dd>Manage providers and environment configuration.</dd>
          </div>
        </dl>

        <div className="onboarding-modal__actions">
          <button
            className="onboarding-modal__dismiss"
            onClick={on_dismiss}
            ref={dismiss_button_ref}
            type="button"
          >
            Continue to workspace
          </button>
        </div>
      </section>
    </div>
  )
}
