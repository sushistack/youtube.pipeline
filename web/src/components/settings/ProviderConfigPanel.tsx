import type { SettingsConfig } from '../../contracts/settingsContracts'

interface ProviderConfigPanelProps {
  config: SettingsConfig
  field_errors: Record<string, string>
  onChange: (field: keyof SettingsConfig, value: string | number | boolean) => void
}

function FieldError({ message }: { message?: string }) {
  if (!message) {
    return null
  }
  return <p className='settings-form__error'>{message}</p>
}

export function ProviderConfigPanel({
  config,
  field_errors,
  onChange,
}: ProviderConfigPanelProps) {
  return (
    <section className='settings-card settings-form-card' aria-labelledby='settings-provider-title'>
      <div className='settings-card__header'>
        <div>
          <p className='route-shell__eyebrow'>Provider controls</p>
          <h2 id='settings-provider-title' className='settings-card__title'>
            Models and cost guardrails
          </h2>
        </div>
      </div>

      <div className='settings-form-grid'>
        {[
          ['writer_provider', 'Writer provider'],
          ['writer_model', 'Writer model'],
          ['critic_provider', 'Critic provider'],
          ['critic_model', 'Critic model'],
          ['image_provider', 'Image provider'],
          ['image_model', 'Image model'],
          ['tts_provider', 'TTS provider'],
          ['tts_model', 'TTS model'],
          ['tts_voice', 'TTS voice'],
          ['tts_audio_format', 'Audio format'],
        ].map(([field, label]) => (
          <label key={field} className='settings-form__field'>
            <span>{label}</span>
            <input
              className='settings-form__control'
              value={config[field as keyof SettingsConfig] as string}
              onChange={(event) => {
                onChange(field as keyof SettingsConfig, event.target.value)
              }}
            />
            <FieldError message={field_errors[`config.${field}`]} />
          </label>
        ))}
      </div>

      <div className='settings-form-grid settings-form-grid--caps'>
        {[
          ['cost_cap_research', 'Research cap'],
          ['cost_cap_write', 'Write cap'],
          ['cost_cap_image', 'Image cap'],
          ['cost_cap_tts', 'TTS cap'],
          ['cost_cap_assemble', 'Assemble cap'],
          ['cost_cap_per_run', 'Run hard cap'],
        ].map(([field, label]) => {
          const raw_value = config[field as keyof SettingsConfig]
          const display_value =
            typeof raw_value === 'number' && !Number.isNaN(raw_value) ? raw_value : ''
          return (
            <label key={field} className='settings-form__field'>
              <span>{label}</span>
              <input
                type='number'
                min='0'
                step='0.01'
                className='settings-form__control'
                value={display_value}
                onChange={(event) => {
                  const raw = event.target.value
                  // Empty input: preserve the previous numeric value instead of
                  // silently sending NaN / 0. Server-side validation will catch
                  // any remaining issues; the UI just shouldn't destroy data on
                  // a transient empty state.
                  if (raw === '') {
                    return
                  }
                  const parsed = Number(raw)
                  if (Number.isNaN(parsed)) {
                    return
                  }
                  onChange(field as keyof SettingsConfig, parsed)
                }}
              />
              <FieldError message={field_errors[`config.${field}`]} />
            </label>
          )
        })}
      </div>

      <label className='settings-form__field settings-form__field--toggle'>
        <input
          type='checkbox'
          className='settings-form__checkbox'
          checked={config.dry_run}
          onChange={(event) => {
            onChange('dry_run', event.target.checked)
          }}
        />
        <span>
          <strong>Dry run (Phase B)</strong>
          <small>
            Skip DashScope image and TTS calls. Generates placeholder assets at
            zero cost. Final video assembly is blocked while enabled.
          </small>
        </span>
      </label>
    </section>
  )
}
