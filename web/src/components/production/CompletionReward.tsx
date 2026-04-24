import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  fetchRunMetadata,
  fetchRunManifest,
} from "../../lib/apiClient";
import { queryKeys } from "../../lib/queryKeys";
import { useNewRunCoordinator } from "./useNewRunCoordinator";
import type { RunSummary } from "../../contracts/runContracts";

interface CompletionRewardProps {
  run: RunSummary;
}

export function CompletionReward({ run }: CompletionRewardProps) {
  const video_ref = useRef<HTMLVideoElement>(null);
  const { open_new_run_panel } = useNewRunCoordinator();

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

  // Artifact JSON is regenerated on resume, so we refetch on mount rather than
  // serving up to 60s of stale data — see ComplianceGate for full rationale.
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

  const metadata = metadata_query.data;
  const manifest = manifest_query.data;

  return (
    <section className="production__completion-reward" aria-label="Run complete">
      <h2 className="production-dashboard__section-title">
        Upload ready
      </h2>
      <p className="route-shell__body">
        The pipeline has finished. The assembled video and compliance metadata
        are ready for upload.
      </p>

      {/* Video reward panel */}
      <div className="completion-reward__video-panel">
        <video
          ref={video_ref}
          src={`/api/runs/${run.id}/video`}
          autoPlay
          muted
          playsInline
          className="completion-reward__video"
        >
          <track kind="captions" />
        </video>
      </div>

      {/* Metadata summary table */}
      <table className="completion-reward__summary-table">
        <caption>Compliance metadata summary</caption>
        <tbody>
          <tr>
            <th scope="row">Title</th>
            <td>{metadata?.title ?? "—"}</td>
          </tr>
          <tr>
            <th scope="row">Source</th>
            <td>{manifest?.source_url ?? "—"}</td>
          </tr>
          <tr>
            <th scope="row">Author</th>
            <td>{manifest?.author_name ?? "—"}</td>
          </tr>
          <tr>
            <th scope="row">License</th>
            <td>{manifest?.license ?? "—"}</td>
          </tr>
        </tbody>
      </table>

      {/* Next-action CTA */}
      <div className="completion-reward__actions">
        <button
          type="button"
          className="completion-reward__start-next-btn"
          onClick={() => {
            open_new_run_panel({ restore_focus_to: null });
          }}
        >
          Start Next SCP
        </button>
        <p className="completion-reward__run-id">
          Run ID: {run.id}
        </p>
      </div>
    </section>
  );
}
