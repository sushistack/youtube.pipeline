import { useState } from 'react'
import type { SettingsConfig } from '../../contracts/settingsContracts'
import { ApiClientError } from '../../lib/apiClient'
import { BudgetIndicator } from '../settings/BudgetIndicator'
import { ProviderConfigPanel } from '../settings/ProviderConfigPanel'
import { SecretFieldsPanel, type SecretDraft } from '../settings/SecretFieldsPanel'
import { TimelineView } from '../settings/TimelineView'
import {
  useSettingsMutation,
  useSettingsQuery,
  useSettingsResetMutation,
} from '../../hooks/useSettings'

function defaultConfig(): SettingsConfig {
  return {
    writer_model: '',
    critic_model: '',
    image_model: '',
    tts_model: '',
    tts_voice: '',
    tts_audio_format: '',
    writer_provider: '',
    critic_provider: '',
    image_provider: '',
    tts_provider: '',
    dashscope_region: '',
    cost_cap_research: 0,
    cost_cap_write: 0,
    cost_cap_image: 0,
    cost_cap_tts: 0,
    cost_cap_assemble: 0,
    cost_cap_per_run: 0,
  }
}

function validateSettings(config: SettingsConfig) {
  const field_errors: Record<string, string> = {}
  const required_fields: Array<keyof SettingsConfig> = [
    'writer_provider',
    'writer_model',
    'critic_provider',
    'critic_model',
    'image_provider',
    'image_model',
    'tts_provider',
    'tts_model',
    'tts_voice',
    'tts_audio_format',
    'dashscope_region',
  ]

  for (const field of required_fields) {
    if (`${config[field]}`.trim().length === 0) {
      field_errors[`config.${field}`] = 'Required'
    }
  }

  if (config.writer_provider === config.critic_provider) {
    field_errors['config.writer_provider'] =
      'Writer and Critic must use different providers'
    field_errors['config.critic_provider'] =
      'Writer and Critic must use different providers'
  }

  const stage_caps = [
    config.cost_cap_research,
    config.cost_cap_write,
    config.cost_cap_image,
    config.cost_cap_tts,
    config.cost_cap_assemble,
  ]
  for (const [field, value] of Object.entries(config)) {
    if (field.startsWith('cost_cap_')) {
      if (typeof value !== 'number' || Number.isNaN(value)) {
        field_errors[`config.${field}`] = 'Must be a number'
      } else if (value < 0) {
        field_errors[`config.${field}`] = 'Must be non-negative'
      }
    }
  }
  if (
    typeof config.cost_cap_per_run === 'number' &&
    !Number.isNaN(config.cost_cap_per_run) &&
    config.cost_cap_per_run < Math.max(...stage_caps)
  ) {
    field_errors['config.cost_cap_per_run'] =
      'Run hard cap must be at least the highest stage cap'
  }

  return field_errors
}

export function SettingsShell() {
  const settings_query = useSettingsQuery()
  const settings_mutation = useSettingsMutation()
  const reset_mutation = useSettingsResetMutation()

  const [draft_config, set_draft_config] = useState<SettingsConfig | null>(null)
  const [secret_drafts, set_secret_drafts] = useState<Record<string, SecretDraft>>({})
  const [field_errors, set_field_errors] = useState<Record<string, string>>({})
  const [submit_state, set_submit_state] = useState<string | null>(null)

  if (settings_query.isPending) {
    return (
      <section className='route-shell' aria-labelledby='settings-shell-title'>
        <p className='route-shell__eyebrow'>Operational controls</p>
        <h1 id='settings-shell-title' className='route-shell__title'>
          Settings
        </h1>
        <p className='route-shell__body'>Loading provider configuration…</p>
      </section>
    )
  }

  if (settings_query.isError || !settings_query.data) {
    const is_corrupted =
      settings_query.error instanceof ApiClientError &&
      settings_query.error.code === 'SETTINGS_CORRUPTED'
    return (
      <section className='route-shell' aria-labelledby='settings-shell-title'>
        <p className='route-shell__eyebrow'>Operational controls</p>
        <h1 id='settings-shell-title' className='route-shell__title'>
          Settings
        </h1>
        {is_corrupted ? (
          <div className='settings-banner settings-banner--error' role='alert'>
            <p>
              <strong>config.yaml is unreadable.</strong> Fix it on disk, or
              reset the non-secret config to defaults to recover. API keys in
              .env are untouched by reset.
            </p>
            <button
              type='button'
              className='settings-workspace__save'
              disabled={reset_mutation.isPending}
              onClick={() => {
                reset_mutation.mutate()
              }}
            >
              {reset_mutation.isPending ? 'Resetting…' : 'Reset to defaults'}
            </button>
          </div>
        ) : (
          <p className='route-shell__body'>
            Failed to load settings. Retry the request from the browser refresh
            or server logs.
          </p>
        )}
      </section>
    )
  }

  const { snapshot, etag } = settings_query.data
  const config = draft_config ?? snapshot.config

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()

    const client_errors = validateSettings(config)
    set_field_errors(client_errors)
    if (Object.keys(client_errors).length > 0) {
      set_submit_state('Fix the highlighted fields before saving.')
      return
    }

    const env_payload: Record<string, string | null> = {}
    for (const [key, draft] of Object.entries(secret_drafts)) {
      if (draft.cleared) {
        env_payload[key] = null
        continue
      }
      if (draft.value != null && draft.value !== '') {
        env_payload[key] = draft.value
      }
    }

    try {
      const next_snapshot = await settings_mutation.mutateAsync({
        config,
        env: env_payload,
        etag,
      })
      set_field_errors({})
      set_secret_drafts({})
      set_draft_config(next_snapshot.config)
      set_submit_state('Settings saved successfully.')
    } catch (error) {
      if (error instanceof ApiClientError) {
        if (error.status === 409) {
          set_submit_state(
            'Another save happened first. Refresh to load the latest settings and retry.',
          )
          // Invalidate so the refetch picks up the new ETag.
          settings_query.refetch()
          return
        }
        if (error.details && typeof error.details === 'object') {
          set_field_errors(error.details as Record<string, string>)
        }
      }
      set_submit_state(
        error instanceof Error ? error.message : 'Failed to save settings.',
      )
    }
  }

  return (
    <section className='route-shell settings-shell' aria-labelledby='settings-shell-title'>
      <div className='settings-shell__hero'>
        <div>
          <p className='route-shell__eyebrow'>Operational controls</p>
          <h1 id='settings-shell-title' className='route-shell__title'>
            Settings
          </h1>
          <p className='route-shell__body'>
            Manage provider routing, model selections, API keys, and run-budget
            guardrails without leaving the operator workspace.
          </p>
        </div>
        <BudgetIndicator budget={snapshot.budget} />
      </div>

      <form className='settings-workspace' onSubmit={handleSubmit}>
        <div className='settings-workspace__controls'>
          <ProviderConfigPanel
            config={config}
            field_errors={field_errors}
            onChange={(field, value) => {
              set_draft_config((current) => ({
                ...(current ?? snapshot.config ?? defaultConfig()),
                [field]: value,
              }))
            }}
          />
          <SecretFieldsPanel
            env={snapshot.env}
            field_errors={field_errors}
            drafts={secret_drafts}
            onChange={(key, draft) => {
              set_secret_drafts((current) => ({ ...current, [key]: draft }))
            }}
          />
        </div>

        <div className='settings-workspace__actions'>
          <div>
            <p className='settings-workspace__destination'>
              Non-secret values save to <code>config.yaml</code>. API keys save
              to <code>.env</code>.
            </p>
            {submit_state ? (
              <p className='settings-workspace__status' role='status'>
                {submit_state}
              </p>
            ) : null}
          </div>
          <button
            type='submit'
            className='settings-workspace__save'
            disabled={settings_mutation.isPending}
          >
            {settings_mutation.isPending ? 'Saving…' : 'Save settings'}
          </button>
        </div>
      </form>

      <TimelineView />
    </section>
  )
}
