import type { SettingsSnapshot } from '../../contracts/settingsContracts'

export interface SecretDraft {
  value: string
  cleared: boolean
}

interface SecretFieldsPanelProps {
  env: SettingsSnapshot['env']
  field_errors: Record<string, string>
  drafts: Record<string, SecretDraft>
  onChange: (key: string, draft: SecretDraft) => void
}

export function SecretFieldsPanel({
  env,
  field_errors,
  drafts,
  onChange,
}: SecretFieldsPanelProps) {
  const fields = [
    ['DASHSCOPE_API_KEY', 'DashScope API key'],
    ['DEEPSEEK_API_KEY', 'DeepSeek API key'],
    ['GEMINI_API_KEY', 'Gemini API key'],
  ] as const

  return (
    <section className='settings-card settings-form-card' aria-labelledby='settings-secret-title'>
      <div className='settings-card__header'>
        <div>
          <p className='route-shell__eyebrow'>Secret storage</p>
          <h2 id='settings-secret-title' className='settings-card__title'>
            API keys
          </h2>
        </div>
      </div>

      <p className='settings-form-card__body'>
        These values persist to <code>.env</code>. Leave a field blank to keep
        the current secret unchanged. Use <em>Clear</em> to remove the secret
        from <code>.env</code> entirely.
      </p>

      <div className='settings-form-grid settings-form-grid--single'>
        {fields.map(([key, label]) => {
          const draft = drafts[key] ?? { value: '', cleared: false }
          const is_configured = env[key]?.configured ?? false
          let placeholder = is_configured ? 'Configured' : 'Not configured'
          if (draft.cleared) {
            placeholder = 'Will be cleared on save'
          }
          return (
            <label key={key} className='settings-form__field'>
              <span>{label}</span>
              <input
                type='password'
                autoComplete='off'
                aria-label={label}
                className='settings-form__control'
                placeholder={placeholder}
                value={draft.value}
                disabled={draft.cleared}
                onChange={(event) => {
                  onChange(key, { value: event.target.value, cleared: false })
                }}
              />
              <div className='settings-form__secret-actions'>
                <small className='settings-form__hint'>
                  {draft.cleared
                    ? 'Save to remove this key from .env'
                    : is_configured
                      ? 'Stored in .env'
                      : 'Will be added to .env on save'}
                </small>
                {is_configured || draft.cleared ? (
                  <button
                    type='button'
                    className='settings-form__secret-clear'
                    onClick={() => {
                      onChange(key, { value: '', cleared: !draft.cleared })
                    }}
                  >
                    {draft.cleared ? 'Undo clear' : 'Clear'}
                  </button>
                ) : null}
              </div>
              {field_errors[`env.${key}`] ? (
                <p className='settings-form__error'>{field_errors[`env.${key}`]}</p>
              ) : null}
            </label>
          )
        })}
      </div>
    </section>
  )
}
