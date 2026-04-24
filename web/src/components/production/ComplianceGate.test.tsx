import "@testing-library/jest-dom";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/renderWithProviders";
import { queryKeys } from "../../lib/queryKeys";
import { ComplianceGate } from "./ComplianceGate";
import type { RunSummary } from "../../contracts/runContracts";

// Mock the apiClient module.
vi.mock("../../lib/apiClient", () => ({
  acknowledgeMetadata: vi.fn(),
  fetchRunMetadata: vi.fn(),
  fetchRunManifest: vi.fn(),
}));

import {
  acknowledgeMetadata,
  fetchRunMetadata,
  fetchRunManifest,
} from "../../lib/apiClient";

const run: RunSummary = {
  cost_usd: 1.25,
  created_at: "2026-04-19T00:00:00Z",
  duration_ms: 45000,
  human_override: false,
  id: "scp-049-run-1",
  retry_count: 0,
  scp_id: "049",
  stage: "metadata_ack",
  status: "waiting",
  token_in: 1200,
  token_out: 400,
  updated_at: "2026-04-19T00:05:00Z",
};

const mockMetadata = {
  version: 1,
  generated_at: "2026-04-19T00:05:00Z",
  run_id: "scp-049-run-1",
  scp_id: "049",
  title: "SCP-049 Test Video",
  ai_generated: { narration: true, imagery: true, tts: true },
  models_used: {
    "gpt-4": { provider: "openai", model: "gpt-4" },
    "tts-1": { provider: "openai", model: "tts-1", voice: "alloy" },
  },
};

const mockManifest = {
  version: 1,
  generated_at: "2026-04-19T00:05:00Z",
  run_id: "scp-049-run-1",
  scp_id: "049",
  source_url: "https://scp-wiki.wikidot.com/scp-049",
  author_name: "Dr. Example",
  license: "CC BY-SA 3.0",
  license_url: "https://creativecommons.org/licenses/by-sa/3.0/",
  license_chain: [
    {
      component: "SCP article text",
      source_url: "https://scp-wiki.wikidot.com/scp-049",
      author_name: "Dr. Example",
      license: "CC BY-SA 3.0",
    },
  ],
};

describe("ComplianceGate", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(fetchRunMetadata).mockResolvedValue(mockMetadata);
    vi.mocked(fetchRunManifest).mockResolvedValue(mockManifest);
    vi.mocked(acknowledgeMetadata).mockResolvedValue({
      id: run.id,
      scp_id: run.scp_id,
      stage: "complete",
      status: "completed",
      retry_count: 0,
      cost_usd: 1.25,
      token_in: 1200,
      token_out: 400,
      duration_ms: 45000,
      human_override: false,
      created_at: run.created_at,
      updated_at: "2026-04-19T00:06:00Z",
    });
  });

  it("renders the compliance gate title and video element", async () => {
    renderWithProviders(<ComplianceGate run={run} />);

    expect(
      screen.getByText("Pre-Upload Compliance Gate"),
    ).toBeInTheDocument();

    // Video element should be present.
    const video = document.querySelector("video");
    expect(video).toBeInTheDocument();
    expect(video).toHaveAttribute("src", `/api/runs/${run.id}/video`);
  });

  it("renders all checklist items after metadata loads", async () => {
    renderWithProviders(<ComplianceGate run={run} />);

    await waitFor(() => {
      expect(
        screen.getByText(/Title confirmed: SCP-049 Test Video/),
      ).toBeInTheDocument();
    });

    expect(
      screen.getByText(/AI disclosure — Narration: AI/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/AI disclosure — Imagery: AI/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/AI disclosure — TTS: AI/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Models logged: gpt-4, tts-1/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Source URL confirmed: https:\/\/scp-wiki/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Author confirmed: Dr. Example/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/License: CC BY-SA 3.0/),
    ).toBeInTheDocument();
  });

  it("disables the Finalize button when no checkboxes are checked", async () => {
    renderWithProviders(<ComplianceGate run={run} />);

    await waitFor(() => {
      expect(
        screen.getByText(/Title confirmed: SCP-049 Test Video/),
      ).toBeInTheDocument();
    });

    const btn = screen.getByRole("button", {
      name: /Acknowledge & Complete/,
    });
    expect(btn).toBeDisabled();
  });

  it("enables the Finalize button when all checkboxes are checked", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ComplianceGate run={run} />);

    await waitFor(() => {
      expect(
        screen.getByText(/Title confirmed: SCP-049 Test Video/),
      ).toBeInTheDocument();
    });

    // Check all checkboxes.
    const checkboxes = screen.getAllByRole("checkbox");
    for (const cb of checkboxes) {
      await user.click(cb);
    }

    const btn = screen.getByRole("button", {
      name: /Acknowledge & Complete/,
    });
    expect(btn).toBeEnabled();
  });

  it("calls acknowledgeMetadata on finalize and shows pending state", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ComplianceGate run={run} />);

    await waitFor(() => {
      expect(
        screen.getByText(/Title confirmed: SCP-049 Test Video/),
      ).toBeInTheDocument();
    });

    // Check all checkboxes.
    const checkboxes = screen.getAllByRole("checkbox");
    for (const cb of checkboxes) {
      await user.click(cb);
    }

    // Click finalize.
    const btn = screen.getByRole("button", {
      name: /Acknowledge & Complete/,
    });
    await user.click(btn);

    await waitFor(() => {
      expect(acknowledgeMetadata).toHaveBeenCalledWith(run.id);
    });
  });

  it("invalidates status and list queries after successful ack", async () => {
    const user = userEvent.setup();
    const { queryClient } = renderWithProviders(<ComplianceGate run={run} />);
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");

    await waitFor(() => {
      expect(
        screen.getByText(/Title confirmed: SCP-049 Test Video/),
      ).toBeInTheDocument();
    });

    for (const cb of screen.getAllByRole("checkbox")) {
      await user.click(cb);
    }
    await user.click(
      screen.getByRole("button", { name: /Acknowledge & Complete/ }),
    );

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: queryKeys.runs.status(run.id),
      });
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: queryKeys.runs.list(),
      });
    });
  });

  it("keeps the Finalize button disabled after a successful ack to prevent double-submit", async () => {
    const user = userEvent.setup();
    renderWithProviders(<ComplianceGate run={run} />);

    await waitFor(() => {
      expect(
        screen.getByText(/Title confirmed: SCP-049 Test Video/),
      ).toBeInTheDocument();
    });

    for (const cb of screen.getAllByRole("checkbox")) {
      await user.click(cb);
    }
    await user.click(
      screen.getByRole("button", { name: /Acknowledge & Complete/ }),
    );

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /Acknowledged/ })).toBeDisabled();
    });
  });

  it("shows the video-unavailable banner only when the video element errors", async () => {
    renderWithProviders(<ComplianceGate run={run} />);

    await waitFor(() => {
      expect(
        screen.getByText(/Title confirmed: SCP-049 Test Video/),
      ).toBeInTheDocument();
    });

    // Metadata loaded successfully — banner should NOT appear even though metadata query is fine.
    expect(screen.queryByText(/Video not yet available/)).not.toBeInTheDocument();

    // Simulate the video element failing to load.
    const video = document.querySelector("video") as HTMLVideoElement;
    video.dispatchEvent(new Event("error"));

    await waitFor(() => {
      expect(screen.getByText(/Video not yet available/)).toBeInTheDocument();
    });
  });
});
