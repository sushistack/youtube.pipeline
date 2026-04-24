import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  acknowledgeMetadata,
  fetchRunMetadata,
  fetchRunManifest,
} from "../../lib/apiClient";
import { queryKeys } from "../../lib/queryKeys";
import type { RunSummary } from "../../contracts/runContracts";

interface ComplianceGateProps {
  run: RunSummary;
}

/** Checklist item IDs used for tracking checkbox state. */
const CHECKLIST_ITEMS = [
  "title_confirmed",
  "ai_disclosure_narration",
  "ai_disclosure_imagery",
  "ai_disclosure_tts",
  "models_logged",
  "source_url_confirmed",
  "author_confirmed",
  "license_confirmed",
] as const;

type ChecklistId = (typeof CHECKLIST_ITEMS)[number];

export function ComplianceGate({ run }: ComplianceGateProps) {
  const query_client = useQueryClient();
  const video_ref = useRef<HTMLVideoElement>(null);
  const [checklist, set_checklist] = useState<Record<ChecklistId, boolean>>(
    () => Object.fromEntries(CHECKLIST_ITEMS.map((id) => [id, false])) as Record<ChecklistId, boolean>,
  );
  const [error_message, set_error_message] = useState<string | null>(null);
  const [video_load_failed, set_video_load_failed] = useState(false);

  // 5-second video auto-stop.
  useEffect(() => {
    const video = video_ref.current;
    if (!video) return;
    const onTimeUpdate = () => {
      if (video.currentTime >= 5) {
        video.pause();
      }
    };
    video.addEventListener("timeupdate", onTimeUpdate);
    return () => video.removeEventListener("timeupdate", onTimeUpdate);
  }, [run.id]);

  // Artifact JSON is regenerated on resume (see internal/pipeline/resume.go),
  // so we never want cached data to outlive the current mount — `staleTime: 0`
  // forces a refetch every time the operator lands on the gate.
  const metadata_query = useQuery({
    queryFn: () => fetchRunMetadata(run.id),
    queryKey: queryKeys.runs.metadata(run.id),
    staleTime: 0,
    retry: false,
  });

  const manifest_query = useQuery({
    queryFn: () => fetchRunManifest(run.id),
    queryKey: queryKeys.runs.manifest(run.id),
    staleTime: 0,
    retry: false,
  });

  const ack_mutation = useMutation({
    mutationFn: () => acknowledgeMetadata(run.id),
    onMutate: () => {
      set_error_message(null);
    },
    onSuccess: () => {
      set_error_message(null);
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      });
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.list(),
      });
    },
    onError: (err: Error) => {
      set_error_message(err.message);
    },
  });

  function toggleCheckbox(id: ChecklistId) {
    set_checklist((prev) => ({ ...prev, [id]: !prev[id] }));
  }

  const all_checked = CHECKLIST_ITEMS.every((id) => checklist[id]);
  const is_loading =
    metadata_query.isLoading || manifest_query.isLoading;
  const has_fetch_error =
    (metadata_query.isError || manifest_query.isError) && !is_loading;

  // Build checklist labels from fetched data.
  function checklistLabel(id: ChecklistId): string {
    const metadata = metadata_query.data;
    const manifest = manifest_query.data;

    switch (id) {
      case "title_confirmed":
        return `Title confirmed: ${metadata?.title ?? "—"}`;
      case "ai_disclosure_narration":
        return `AI disclosure — Narration: ${metadata?.ai_generated.narration ? "AI" : "Human"}`;
      case "ai_disclosure_imagery":
        return `AI disclosure — Imagery: ${metadata?.ai_generated.imagery ? "AI" : "Human"}`;
      case "ai_disclosure_tts":
        return `AI disclosure — TTS: ${metadata?.ai_generated.tts ? "AI" : "Human"}`;
      case "models_logged":
        return `Models logged: ${metadata ? Object.keys(metadata.models_used).join(", ") : "—"}`;
      case "source_url_confirmed":
        return `Source URL confirmed: ${manifest?.source_url ?? "—"}`;
      case "author_confirmed":
        return `Author confirmed: ${manifest?.author_name ?? "—"}`;
      case "license_confirmed":
        return `License: ${manifest?.license ?? "—"}`;
    }
  }

  return (
    <section className="production__compliance-gate" aria-label="Compliance gate">
      <h2 className="production-dashboard__section-title">Pre-Upload Compliance Gate</h2>
      <p className="route-shell__body">
        Review the assembled video and confirm metadata before finalising the
        upload.
      </p>

      {/* Video preview */}
      <div className="compliance-gate__video-panel">
        <video
          ref={video_ref}
          src={`/api/runs/${run.id}/video`}
          autoPlay
          muted
          playsInline
          className="compliance-gate__video"
          onError={() => set_video_load_failed(true)}
          onLoadedData={() => set_video_load_failed(false)}
        >
          <track kind="captions" />
        </video>
        {video_load_failed && (
          <div className="compliance-gate__video-warning" role="alert">
            <strong>Video not yet available</strong>
            <p>The assembled video could not be loaded. You may still proceed.</p>
          </div>
        )}
      </div>

      {/* Metadata checklist */}
      <fieldset className="compliance-gate__checklist">
        <legend>Verification checklist</legend>

        {is_loading ? (
          <div className="compliance-gate__skeleton" aria-busy="true">
            {CHECKLIST_ITEMS.map((id) => (
              <label key={id} className="compliance-gate__skeleton-item">
                <span className="compliance-gate__skeleton-checkbox" />
                <span className="compliance-gate__skeleton-label" />
              </label>
            ))}
          </div>
        ) : (
          CHECKLIST_ITEMS.map((id) => (
            <label key={id} className="compliance-gate__checkbox-label">
              <input
                type="checkbox"
                checked={checklist[id]}
                onChange={() => toggleCheckbox(id)}
              />
              <span>{checklistLabel(id)}</span>
            </label>
          ))
        )}

        {has_fetch_error && (
          <div className="compliance-gate__fetch-error" role="alert">
            <strong>Metadata could not be loaded</strong>
            <p>
              Some details are unavailable. Verify the information out-of-band
              before proceeding.
            </p>
          </div>
        )}
      </fieldset>

      {/* Error banner */}
      {error_message && (
        <div className="compliance-gate__error" role="alert">
          {error_message}
        </div>
      )}

      {/* Finalize button */}
      <button
        type="button"
        className="compliance-gate__finalize-btn"
        disabled={
          !all_checked || ack_mutation.isPending || ack_mutation.isSuccess
        }
        onClick={() => ack_mutation.mutate()}
      >
        {ack_mutation.isPending
          ? "Finalising…"
          : ack_mutation.isSuccess
            ? "Acknowledged"
            : "Acknowledge & Complete"}
      </button>
    </section>
  );
}
