import type { ReviewItem } from '../../contracts/runContracts'

interface SceneCardProps {
  is_locally_approved?: boolean
  is_regenerating?: boolean
  item: ReviewItem
  on_select: () => void
  selected: boolean
}

function scoreTone(score: number | null | undefined) {
  if (score == null) {
    return 'muted'
  }
  if (score >= 80) {
    return 'high'
  }
  if (score >= 50) {
    return 'mid'
  }
  return 'low'
}

function formatScore(score: number | null | undefined) {
  if (score == null) {
    return 'No score'
  }
  return `${Math.round(score)}`
}

function reviewStateLabel(
  review_status: ReviewItem['review_status'],
  is_regenerating: boolean,
  is_locally_approved: boolean,
) {
  if (is_regenerating) {
    return 'Regenerating'
  }
  if (is_locally_approved) {
    return 'Approved'
  }
  switch (review_status) {
    case 'waiting_for_review':
      return 'Pending'
    case 'auto_approved':
      return 'Auto-approved'
    case 'approved':
      return 'Approved'
    case 'rejected':
      return 'Rejected'
    default:
      return 'Pending'
  }
}

export function SceneCard({
  is_locally_approved = false,
  is_regenerating = false,
  item,
  on_select,
  selected,
}: SceneCardProps) {
  const status_key = is_regenerating
    ? 'regenerating'
    : is_locally_approved
    ? 'approved'
    : item.review_status

  return (
    <button
      type="button"
      className="scene-card"
      data-selected={String(selected)}
      data-regenerating={String(is_regenerating)}
      data-retry-exhausted={String(item.retry_exhausted)}
      onClick={on_select}
      aria-selected={selected}
      role="option"
    >
      <div className="scene-card__index" aria-hidden="true">
        S{item.scene_index + 1}
      </div>
      <div className="scene-card__body">
        <div className="scene-card__title-row">
          <p className="scene-card__title">Scene {item.scene_index + 1}</p>
          {item.high_leverage ? (
            <span className="scene-card__chip scene-card__chip--high-leverage">
              High-Leverage
            </span>
          ) : null}
          {is_regenerating ? (
            <span
              className="scene-card__chip scene-card__chip--regenerating"
              aria-label="Regenerating"
            >
              Regenerating…
            </span>
          ) : null}
          {item.retry_exhausted ? (
            <span
              className="scene-card__chip scene-card__chip--exhausted"
              aria-label="Retry exhausted"
            >
              Retry exhausted
            </span>
          ) : null}
        </div>
        <p className="scene-card__excerpt">{item.narration || 'No narration available.'}</p>
      </div>
      <div className="scene-card__meta">
        <span className="scene-card__score" data-tone={scoreTone(item.critic_score)}>
          {formatScore(item.critic_score)}
        </span>
        <span className="scene-card__status" data-status={status_key}>
          {reviewStateLabel(item.review_status, is_regenerating, is_locally_approved)}
        </span>
      </div>
    </button>
  )
}
