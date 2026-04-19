import { TimelineView } from '../settings/TimelineView'

export function SettingsShell() {
  return (
    <section className="route-shell" aria-labelledby="settings-shell-title">
      <p className="route-shell__eyebrow">Operational controls</p>
      <h1 id="settings-shell-title" className="route-shell__title">
        Settings
      </h1>
      <p className="route-shell__body">
        Manage application preferences, provider configuration, and shell-level
        defaults for local operator workflows.
      </p>
      <TimelineView />
    </section>
  )
}
