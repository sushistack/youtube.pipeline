export interface SaveRecommendationBannerProps {
  versionTag: string
  goldenPassedInSession: boolean
  onDismiss: () => void
  onRunShadow: () => void
}

export function SaveRecommendationBanner({
  versionTag,
  goldenPassedInSession,
  onDismiss,
  onRunShadow,
}: SaveRecommendationBannerProps) {
  return (
    <div
      className="tuning-banner tuning-banner--recommendation"
      role="status"
      aria-label="Save recommendation"
    >
      <p>
        Prompt saved as <code>{versionTag}</code>. Next step: run Shadow eval
        to confirm the new prompt doesn&apos;t regress recent runs.
      </p>
      {!goldenPassedInSession ? (
        <p className="tuning-banner__detail">
          Required order: <strong>save → Golden → Shadow</strong>. Run Golden
          first to unlock Shadow.
        </p>
      ) : null}
      <div className="tuning-banner__actions">
        <button
          type="button"
          className="tuning-button tuning-button--primary"
          disabled={!goldenPassedInSession}
          onClick={onRunShadow}
        >
          Run Shadow now
        </button>
        <button
          type="button"
          className="tuning-button tuning-button--ghost"
          onClick={onDismiss}
        >
          Dismiss
        </button>
      </div>
    </div>
  )
}
