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
          <div className="detail-panel__title-row">
            <h2 className="detail-panel__title">Scene {item.scene_index + 1}</h2>
            {(() => {
              const aggregate =
                item.critic_breakdown?.aggregate_score ?? item.critic_score ?? null
              if (aggregate == null) {
                return null
              }
              return (
                <span
                  className="detail-panel__aggregate-score"
                  data-tone={scoreTone(aggregate)}
                  aria-label={`Aggregate critic score ${Math.round(aggregate)}`}
                >
                  {Math.round(aggregate)}
                </span>
              )
            })()}
            <span
              className="detail-panel__status-badge"
              data-status={item.review_status}
              aria-label={`Review status ${item.review_status}`}
            >
              {item.review_status.replace(/_/g, ' ')}
            </span>
          </div>
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
          <div className="detail-panel__hero-fallback">
          <svg aria-hidden="true" className="detail-panel__hero-placeholder-icon" fill="none" height="40" viewBox="0 0 40 40" width="40">
            <rect height="26" rx="3" stroke="currentColor" strokeWidth="1.5" width="34" x="3" y="9" />
            <circle cx="13" cy="19" r="3.5" stroke="currentColor" strokeWidth="1.5" />
            <path d="M3 28l8-7 6 6 5-4 9 8" stroke="currentColor" strokeLinejoin="round" strokeWidth="1.5" />
          </svg>
          <span>No image yet</span>
        </div>
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

        <div className="detail-panel__content">
          <h3 className="detail-panel__section-title">Critic metrics</h3>
          <ul className="detail-panel__metrics-grid">
            {[
              { key: 'visual', label: 'Visual', score: null, missing: true },
              {
                key: 'narration',
                label: 'Narration',
                score: item.critic_breakdown?.hook_strength ?? null,
                missing: false,
              },
              {
                key: 'coherence',
                label: 'Coherence',
                score: item.critic_breakdown?.immersion ?? null,
                missing: false,
              },
              {
                key: 'pacing',
                label: 'Pacing',
                score: item.critic_breakdown?.emotional_variation ?? null,
                missing: false,
              },
              {
                key: 'scp_accuracy',
                label: 'SCP Accuracy',
                score: item.critic_breakdown?.fact_accuracy ?? null,
                missing: false,
              },
              { key: 'audio', label: 'Audio', score: null, missing: true },
            ].map((metric) => (
              <li
                key={metric.key}
                className="detail-panel__metric-card"
                data-metric={metric.key}
                title={
                  metric.missing
                    ? 'metric not yet emitted by critic'
                    : undefined
                }
                aria-label={
                  metric.missing
                    ? `${metric.label}: metric not yet emitted by critic`
                    : metric.score == null
                      ? `${metric.label}: score unavailable`
                      : `${metric.label}: ${Math.round(metric.score)}`
                }
              >
                <span className="detail-panel__metric-label">
                  {metric.label}
                </span>
                <strong
                  className="detail-panel__metric-score"
                  data-tone={
                    metric.missing || metric.score == null
                      ? 'muted'
                      : scoreTone(metric.score)
                  }
                >
                  {metric.missing || metric.score == null
                    ? '—'
                    : `${Math.round(metric.score)}`}
                </strong>
              </li>
            ))}
          </ul>
        </div>
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
