import type { ReviewItem } from '../../contracts/runContracts'

interface SceneCardProps {
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

function reviewStateLabel(review_status: ReviewItem['review_status'], is_regenerating: boolean) {
  if (is_regenerating) {
    return 'Regenerating'
  }
  switch (review_status) {
    case 'waiting_for_review':
      return 'Waiting'
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

export function SceneCard({ is_regenerating = false, item, on_select, selected }: SceneCardProps) {
  const thumbnails = item.shots.slice(0, 5)

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
      <div className="scene-card__header">
        <div>
          <p className="scene-card__eyebrow">Scene {item.scene_index + 1}</p>
          <p className="scene-card__state">{reviewStateLabel(item.review_status, is_regenerating)}</p>
        </div>
        <div className="scene-card__badges">
          {item.high_leverage ? (
            <span className="scene-card__badge scene-card__badge--high-leverage">
              High-Leverage
            </span>
          ) : null}
          {is_regenerating ? (
            <span
              className="scene-card__badge scene-card__badge--regenerating"
              aria-label="Regenerating"
            >
              Regenerating…
            </span>
          ) : null}
          {item.retry_exhausted ? (
            <span
              className="scene-card__badge scene-card__badge--exhausted"
              aria-label="Retry exhausted"
            >
              Retry exhausted
            </span>
          ) : null}
          <span
            className="scene-card__badge"
            data-tone={scoreTone(item.critic_score)}
          >
            {formatScore(item.critic_score)}
          </span>
        </div>
      </div>

      <div className="scene-card__thumbnails" aria-hidden="true">
        {thumbnails.map((shot, index) => (
          <div key={`${item.scene_index}-${index}`} className="scene-card__thumb">
            {shot.image_path ? (
              <img
                alt=""
                className="scene-card__thumb-image"
                src={shot.image_path}
              />
            ) : (
              <div className="scene-card__thumb-fallback">Shot {index + 1}</div>
            )}
          </div>
        ))}
      </div>

      <p className="scene-card__excerpt">{item.narration || 'No narration available.'}</p>
    </button>
  )
}
