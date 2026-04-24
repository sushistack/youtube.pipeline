import type { SettingsSnapshot } from '../../contracts/settingsContracts'

function formatQueuedTime(timestamp?: string) {
  if (!timestamp) {
    return 'Pending activation at the next safe seam.'
  }
  const parsed = new Date(timestamp)
  if (Number.isNaN(parsed.getTime())) {
    return `Queued at ${timestamp}`
  }
  return `Queued at ${parsed.toLocaleString()}`
}

export function QueuedChangeBanner({
  application,
}: {
  application: SettingsSnapshot['application']
}) {
  if (application.status !== 'queued') {
    return (
      <div className='settings-banner settings-banner--effective' role='status'>
        Current settings are fully effective.
      </div>
    )
  }

  return (
    <div className='settings-banner settings-banner--queued' role='status'>
      <strong>Queued change set.</strong> New settings are saved, but active stages
      keep their current inputs until the next stage entry or a fresh run.
      <span className='settings-banner__meta'>{formatQueuedTime(application.queued_at)}</span>
    </div>
  )
}
