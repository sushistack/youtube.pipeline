import { useRef, useState } from 'react'
import { CalibrationSection } from '../tuning/CalibrationSection'
import { CriticPromptSection } from '../tuning/CriticPromptSection'
import { FastFeedbackSection } from '../tuning/FastFeedbackSection'
import { FixtureManagementSection } from '../tuning/FixtureManagementSection'
import { GoldenEvalSection } from '../tuning/GoldenEvalSection'
import { SaveRecommendationBanner } from '../tuning/SaveRecommendationBanner'
import { ShadowEvalSection } from '../tuning/ShadowEvalSection'

export function TuningShell() {
  // AC-6 session gate: Shadow is disabled until Golden passes at least
  // once during the current visit. Refreshing the page resets the gate;
  // the operator has to re-run Golden before Shadow re-opens.
  const [goldenPassedInSession, setGoldenPassedInSession] = useState(false)

  // AC-7 save recommendation: shows until dismissed, until a newer save
  // overwrites it, or until Shadow runs for the same version_tag. We
  // track "shadowed version_tag" separately so dismissing one banner
  // doesn't swallow a future save.
  const [recommendation, setRecommendation] = useState<string | null>(null)
  const shadowSectionRef = useRef<HTMLDivElement>(null)

  return (
    <section
      className="route-shell tuning-shell"
      aria-labelledby="tuning-shell-title"
    >
      <div className="tuning-shell__hero">
        <p className="route-shell__eyebrow">Prompt and rubric lab</p>
        <h1 id="tuning-shell-title" className="route-shell__title">
          Tuning
        </h1>
        <p className="route-shell__body">
          Edit the Critic prompt, replay it against Golden and Shadow
          evaluations, and watch the calibration trend respond.
        </p>
      </div>

      {recommendation ? (
        <SaveRecommendationBanner
          versionTag={recommendation}
          goldenPassedInSession={goldenPassedInSession}
          onDismiss={() => setRecommendation(null)}
          onRunShadow={() => {
            shadowSectionRef.current?.scrollIntoView({ behavior: 'smooth' })
          }}
        />
      ) : null}

      <div className="tuning-shell__sections">
        <CriticPromptSection
          onSaved={(envelope) => {
            if (envelope.version_tag) {
              setRecommendation(envelope.version_tag)
            }
          }}
        />
        <FastFeedbackSection />
        <GoldenEvalSection
          onGoldenPassed={() => setGoldenPassedInSession(true)}
        />
        <div ref={shadowSectionRef}>
          <ShadowEvalSection
            goldenPassedInSession={goldenPassedInSession}
            onShadowCompleted={(report) => {
              // Clear the banner only when Shadow replayed the version
              // the banner is advertising. A later save re-sets it.
              if (
                recommendation &&
                report.version_tag &&
                recommendation === report.version_tag
              ) {
                setRecommendation(null)
              }
            }}
          />
        </div>
        <FixtureManagementSection />
        <CalibrationSection />
      </div>
    </section>
  )
}
