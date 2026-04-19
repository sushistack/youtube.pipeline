import { useState } from 'react'
import { BadgeDollarSign, TimerReset } from 'lucide-react'
import type { RunStatusPayload, RunSummary } from '../../lib/formatters'
import {
  formatCurrency,
  formatElapsed,
  formatTokenCount,
  getStageLabel,
  isRunLive,
  mapStageToNode,
} from '../../lib/formatters'
import { StageStepper } from './StageStepper'

interface StatusBarProps {
  run: RunSummary | null
  status_payload?: RunStatusPayload
}

export function StatusBar({ run, status_payload }: StatusBarProps) {
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  const visible = Boolean(run && isRunLive(run.status))
  const expanded = visible && (hovered || focused)

  return (
    <section
      className="status-bar"
      data-testid="status-bar"
      data-expanded={String(expanded)}
      data-visible={String(visible)}
      aria-hidden={!visible}
      onMouseEnter={() => {
        setHovered(true)
      }}
      onMouseLeave={() => {
        setHovered(false)
      }}
    >
      {run ? (
        <div
          className="status-bar__surface"
          tabIndex={visible ? 0 : -1}
          onBlur={(event) => {
            if (!event.currentTarget.contains(event.relatedTarget)) {
              setFocused(false)
            }
          }}
          onFocus={() => {
            setFocused(true)
          }}
        >
          <div className="status-bar__compact">
            <div className="status-bar__stage">
              <span className="status-bar__stage-label">
                {getStageLabel(run.stage)}
              </span>
              <span className="status-bar__stage-node">
                {mapStageToNode(run.stage)}
              </span>
            </div>
            <div className="status-bar__metrics" aria-label="Live telemetry">
              <span className="status-bar__metric">
                <TimerReset aria-hidden="true" size={14} />
                {formatElapsed(run.duration_ms)}
              </span>
              <span className="status-bar__metric">
                <BadgeDollarSign aria-hidden="true" size={14} />
                {formatCurrency(run.cost_usd)}
              </span>
            </div>
          </div>

          <div className="status-bar__detail" aria-hidden={!expanded}>
            <span className="status-bar__run-id">{run.id}</span>
            <span>
              In {formatTokenCount(run.token_in)} / Out {formatTokenCount(run.token_out)}
            </span>
            {status_payload?.decisions_summary ? (
              <span>
                {status_payload.decisions_summary.approved_count} approved /{' '}
                {status_payload.decisions_summary.pending_count} pending
              </span>
            ) : null}
          </div>

          <div className="status-bar__stepper">
            <StageStepper stage={run.stage} status={run.status} variant="compact" />
          </div>
        </div>
      ) : null}
    </section>
  )
}
