import { useState } from 'react'
import type { ReviewItem } from '../../contracts/runContracts'
import { AudioPlayer } from './AudioPlayer'

interface DetailPanelProps {
  is_regenerating?: boolean
  item: ReviewItem
}

interface DiffPart {
  changed: boolean
  text: string
}

function buildDiffParts(current: string, previous: string): DiffPart[] {
  const currentParts = current.split(/\s+/).filter(Boolean)
  const previousSet = new Set(previous.split(/\s+/).filter(Boolean))

  return currentParts.map((part) => ({
    changed: !previousSet.has(part),
    text: part,
  }))
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

function scoreLabel(score: number | null | undefined) {
  if (score == null) {
    return 'N/A'
  }
  return `${Math.round(score)}`
}

export function DetailPanel({ is_regenerating = false, item }: DetailPanelProps) {
  const [version, setVersion] = useState<'current' | 'previous'>('current')
  const activeVersion = version === 'previous' && item.previous_version ? item.previous_version : item
  const heroShot = activeVersion.shots[0] ?? item.shots[0]
  const narrationDiff = item.previous_version
    ? buildDiffParts(item.narration, item.previous_version.narration)
    : []

  return (
    <article
      className="detail-panel"
      aria-label={`Scene ${item.scene_index + 1} detail`}
      data-regenerating={String(is_regenerating)}
      data-retry-exhausted={String(Boolean(item.retry_exhausted))}
    >
      {is_regenerating ? (
        <div
          className="detail-panel__regenerating"
          role="status"
          aria-live="polite"
          aria-label="Regeneration in progress"
        >
          Regenerating scene {item.scene_index + 1}… other scenes remain reviewable.
        </div>
      ) : item.retry_exhausted ? (
        <div
          className="detail-panel__retry-exhausted"
          role="status"
          aria-live="polite"
          aria-label="Retry cap reached"
        >
          Retry exhausted — manual edit or skip &amp; flag required for scene {item.scene_index + 1}.
        </div>
      ) : null}
      <header className="detail-panel__header">
        <div>
          <p className="detail-panel__eyebrow">Focused review</p>
          <h2 className="detail-panel__title">Scene {item.scene_index + 1}</h2>
          {item.high_leverage_reason ? (
            <p className="detail-panel__reason">Why high-leverage: {item.high_leverage_reason}</p>
          ) : null}
        </div>
        {item.previous_version ? (
          <div className="detail-panel__toggle" role="tablist" aria-label="Version toggle">
            <button
              type="button"
              className="detail-panel__toggle-button"
              data-active={String(version === 'current')}
              onClick={() => setVersion('current')}
            >
              Current
            </button>
            <button
              type="button"
              className="detail-panel__toggle-button"
              data-active={String(version === 'previous')}
              onClick={() => setVersion('previous')}
            >
              Previous
            </button>
          </div>
        ) : null}
      </header>

      <section
        className="detail-panel__hero"
        data-variant={item.high_leverage ? 'high-leverage' : 'default'}
      >
        {item.clip_path && version === 'current' ? (
          <video className="detail-panel__video" controls src={item.clip_path}>
            <track kind="captions" />
          </video>
        ) : heroShot?.image_path ? (
          <img
            alt={`Scene ${item.scene_index + 1} hero`}
            className="detail-panel__hero-image"
            src={heroShot.image_path}
          />
        ) : (
          <div className="detail-panel__hero-fallback">No clip or hero image yet</div>
        )}
      </section>

      <section className="detail-panel__body">
        <AudioPlayer
          key={item.scene_index}
          duration_ms={item.tts_duration_ms}
          scene_key={item.scene_index}
          src={item.tts_path}
        />

        <div className="detail-panel__content">
          <h3 className="detail-panel__section-title">Narration</h3>
          <p className="detail-panel__narration">{activeVersion.narration}</p>
        </div>

        {item.critic_breakdown ? (
          <div className="detail-panel__content">
            <h3 className="detail-panel__section-title">Critic breakdown</h3>
            <ul className="detail-panel__scores">
              {[
                ['Aggregate', item.critic_breakdown.aggregate_score ?? item.critic_score ?? null],
                ['Hook strength', item.critic_breakdown.hook_strength ?? null],
                ['Fact accuracy', item.critic_breakdown.fact_accuracy ?? null],
                ['Emotional variation', item.critic_breakdown.emotional_variation ?? null],
                ['Immersion', item.critic_breakdown.immersion ?? null],
              ].map(([label, score]) => (
                <li key={String(label)} className="detail-panel__score-row">
                  <span>{label}</span>
                  <strong data-tone={scoreTone(score as number | null)}>{scoreLabel(score as number | null)}</strong>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </section>

      {item.previous_version ? (
        <section className="detail-panel__diff" aria-label="Before and after diff">
          <div className="detail-panel__diff-copy">
            <h3 className="detail-panel__section-title">Narration changes</h3>
            <p className="detail-panel__diff-before">Before: {item.previous_version.narration}</p>
            <p className="detail-panel__diff-after">
              After:{' '}
              {narrationDiff.map((part, index) => (
                <span
                  key={`${part.text}-${index}`}
                  className="detail-panel__diff-token"
                  data-changed={String(part.changed)}
                >
                  {part.text}{' '}
                </span>
              ))}
            </p>
          </div>

          <div className="detail-panel__diff-images">
            {([
              ['Before', item.previous_version.shots[0]],
              ['After', item.shots[0]],
            ] as const).map(([label, shot]) => (
              <figure key={String(label)} className="detail-panel__diff-figure">
                {shot?.image_path ? (
                  <img alt={`${label} scene preview`} src={shot.image_path} />
                ) : (
                  <div className="detail-panel__hero-fallback">{label} image unavailable</div>
                )}
                <figcaption>{label}</figcaption>
              </figure>
            ))}
          </div>
        </section>
      ) : null}

      {!item.clip_path ? (
        <section className="detail-panel__gallery" aria-label="Shot gallery">
          {activeVersion.shots.map((shot, index) => (
            <div key={`${item.scene_index}-shot-${index}`} className="detail-panel__gallery-item">
              {shot.image_path ? (
                <img
                  alt={`Scene ${item.scene_index + 1} shot ${index + 1}`}
                  src={shot.image_path}
                />
              ) : (
                <div className="detail-panel__hero-fallback">Shot {index + 1}</div>
              )}
              <div className="detail-panel__gallery-meta">
                <span>Shot {index + 1}</span>
                {shot.transition ? (
                  <span className="detail-panel__transition-chip">{shot.transition}</span>
                ) : null}
              </div>
            </div>
          ))}
        </section>
      ) : null}
    </article>
  )
}
