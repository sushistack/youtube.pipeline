import "@testing-library/jest-dom";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/renderWithProviders";
import { NewRunCoordinatorProvider } from "./NewRunContext";
import { CompletionReward } from "./CompletionReward";
import type { RunSummary } from "../../contracts/runContracts";

// Mock the apiClient module.
vi.mock("../../lib/apiClient", () => ({
  fetchRunMetadata: vi.fn(),
  fetchRunManifest: vi.fn(),
}));

import { fetchRunMetadata, fetchRunManifest } from "../../lib/apiClient";

const run: RunSummary = {
  cost_usd: 1.25,
  created_at: "2026-04-19T00:00:00Z",
  duration_ms: 45000,
  human_override: false,
  id: "scp-049-run-1",
  retry_count: 0,
  scp_id: "049",
  stage: "complete",
  status: "completed",
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

function renderCompletionReward() {
  return renderWithProviders(
    <NewRunCoordinatorProvider>
      <CompletionReward run={run} />
    </NewRunCoordinatorProvider>,
  );
}

describe("CompletionReward", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(fetchRunMetadata).mockResolvedValue(mockMetadata);
    vi.mocked(fetchRunManifest).mockResolvedValue(mockManifest);
  });

  it("renders the completion title and video element", async () => {
    renderCompletionReward();

    expect(
      screen.getByText("Upload ready"),
    ).toBeInTheDocument();

    // Video element should be present.
    const video = document.querySelector("video");
    expect(video).toBeInTheDocument();
    expect(video).toHaveAttribute("src", `/api/runs/${run.id}/video`);
  });

  it("renders metadata summary table after loading", async () => {
    renderCompletionReward();

    await waitFor(() => {
      expect(screen.getByText("SCP-049 Test Video")).toBeInTheDocument();
    });

    expect(
      screen.getByText("https://scp-wiki.wikidot.com/scp-049"),
    ).toBeInTheDocument();
    expect(screen.getByText("Dr. Example")).toBeInTheDocument();
    expect(screen.getByText("CC BY-SA 3.0")).toBeInTheDocument();
  });

  it("renders Run ID text", async () => {
    renderCompletionReward();

    expect(
      screen.getByText(`Run ID: ${run.id}`),
    ).toBeInTheDocument();
  });

  it("renders Start Next SCP button", async () => {
    renderCompletionReward();

    const btn = screen.getByRole("button", {
      name: /Start Next SCP/,
    });
    expect(btn).toBeInTheDocument();
  });
});
