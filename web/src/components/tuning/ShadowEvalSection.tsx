import { useState } from "react";
import type { ShadowReport } from "../../contracts/tuningContracts";
import { ApiClientError } from "../../lib/apiClient";
import { useShadowRunMutation } from "../../hooks/useTuning";

export interface ShadowEvalSectionProps {
  /**
   * Session gate from AC-6: Shadow is only runnable after Golden has
   * passed in the current Tuning session. The parent shell tracks this.
   */
  goldenPassedInSession: boolean;
  /**
   * Called after a Shadow run completes so the parent shell can clear the
   * save-recommendation banner for the version just replayed (AC-7).
   */
  onShadowCompleted?: (report: ShadowReport) => void;
}

export function ShadowEvalSection({
  goldenPassedInSession,
  onShadowCompleted,
}: ShadowEvalSectionProps) {
  const mutation = useShadowRunMutation();
  const [report, setReport] = useState<ShadowReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function handleRun() {
    setError(null);
    try {
      const r = await mutation.mutateAsync();
      setReport(r);
      onShadowCompleted?.(r);
    } catch (err) {
      setReport(null);
      setError(
        err instanceof ApiClientError
          ? err.message
          : err instanceof Error
            ? err.message
            : "Shadow eval failed.",
      );
    }
  }

  const disabled = !goldenPassedInSession || mutation.isPending;

  return (
    <section className="tuning-section" aria-labelledby="tuning-shadow-heading">
      <div className="tuning-section__header">
        <h2 id="tuning-shadow-heading" className="tuning-section__title">
          Shadow Eval
        </h2>
        <p className="tuning-section__meta">
          Replay recent completed runs against the current Critic prompt.
        </p>
      </div>
      {!goldenPassedInSession ? (
        <p
          className="tuning-gating-notice"
          role="note"
          aria-label="Shadow prerequisite"
        >
          Golden must pass this session before Shadow can run.
        </p>
      ) : null}
      <div className="tuning-section__actions">
        <button
          type="button"
          className="tuning-button tuning-button--primary"
          disabled={disabled}
          onClick={handleRun}
        >
          {mutation.isPending ? "Running…" : "Run Shadow eval"}
        </button>
      </div>
      {error ? (
        <p className="tuning-section__error" role="alert">
          {error}
        </p>
      ) : null}
      {report ? (
        <div className="tuning-report">
          <p className="tuning-report__summary">{report.summary_line}</p>
          {report.critic_provider ? (
            <p className="tuning-section__meta">
              Critic runtime <code>{report.critic_provider}</code>
              {report.critic_model ? (
                <>
                  {" "}
                  · model <code>{report.critic_model}</code>
                </>
              ) : null}
            </p>
          ) : null}
          {report.empty ? (
            <p className="tuning-report__reason">
              No recent passed runs were available to replay.
            </p>
          ) : report.false_rejections > 0 ? (
            <p className="tuning-verdict tuning-verdict--retry" role="alert">
              Regression: {report.false_rejections} false rejection
              {report.false_rejections === 1 ? "" : "s"}.
            </p>
          ) : (
            <p className="tuning-verdict tuning-verdict--pass">
              No regressions detected.
            </p>
          )}
          {report.results.length > 0 ? (
            <ul className="tuning-report__rows">
              {report.results.map((row) => (
                <li key={row.run_id} className="tuning-report__row">
                  <code>{row.run_id}</code>{" "}
                  <span
                    className={`tuning-verdict tuning-verdict--${row.new_verdict}`}
                  >
                    {row.new_verdict}
                  </span>{" "}
                  diff {row.overall_diff.toFixed(2)}
                  {row.new_critic_provider ? (
                    <span className="tuning-report__reason">
                      {" "}
                      · {row.new_critic_provider}
                      {row.new_critic_model ? `/${row.new_critic_model}` : ""}
                    </span>
                  ) : null}
                  {row.false_rejection ? (
                    <span className="tuning-report__reason">
                      {" "}
                      · false rejection
                    </span>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
