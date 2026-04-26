import "@testing-library/jest-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "../../test/renderWithProviders";
import { TuningShell } from "./TuningShell";

function installTuningFetchMock(
  options: {
    goldenFalseRejects?: number;
    shadowEmpty?: boolean;
    shadowVersionTag?: string;
  } = {},
) {
  const {
    goldenFalseRejects = 0,
    shadowEmpty = true,
    shadowVersionTag = "20260424T000000Z-abc1234",
  } = options;
  let saveCount = 0;

  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.toString()
          : input.url;
    const method = init?.method ?? "GET";

    if (url.endsWith("/api/tuning/critic-prompt") && method === "GET") {
      return new Response(
        JSON.stringify({
          version: 1,
          data: {
            body: "# Critic prompt\n\nseed content.\n",
            saved_at: "",
            prompt_hash: "abc123def456",
            git_short_sha: "abc1234",
            version_tag: "",
          },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }

    if (url.endsWith("/api/tuning/critic-prompt") && method === "PUT") {
      saveCount += 1;
      return new Response(
        JSON.stringify({
          version: 1,
          data: {
            body: "# Critic prompt\n\nedited\n",
            saved_at: "2026-04-24T03:15:22Z",
            prompt_hash: "def789",
            git_short_sha: "abc1234",
            version_tag: `20260424T0315${saveCount}Z-abc1234`,
          },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }

    if (url.endsWith("/api/tuning/golden") && method === "GET") {
      return new Response(
        JSON.stringify({
          version: 1,
          data: {
            pairs: [],
            pair_count: 0,
            freshness: {
              warnings: [],
              days_since_refresh: 0,
              prompt_hash_changed: false,
              current_prompt_hash: "abc123def456",
            },
            last_report: null,
          },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }

    if (url.endsWith("/api/tuning/golden/run") && method === "POST") {
      return new Response(
        JSON.stringify({
          version: 1,
          data: {
            recall: 1.0,
            total_negative: 3,
            detected_negative: 3,
            false_rejects: goldenFalseRejects,
          },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }

    if (url.endsWith("/api/tuning/shadow/run") && method === "POST") {
      return new Response(
        JSON.stringify({
          version: 1,
          data: {
            window: 20,
            evaluated: shadowEmpty ? 0 : 3,
            false_rejections: 0,
            empty: shadowEmpty,
            summary_line:
              "shadow eval: window=20 evaluated=0 false_rejections=0",
            critic_provider: "deepseek",
            critic_model: "deepseek-chat",
            results: [],
            version_tag: shadowVersionTag,
          },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }

    if (url.includes("/api/tuning/calibration")) {
      return new Response(
        JSON.stringify({
          version: 1,
          data: {
            window: 20,
            limit: 30,
            points: [],
            latest: null,
          },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }

    return new Response(
      JSON.stringify({
        version: 1,
        error: { code: "NOT_FOUND", message: "no mock", recoverable: false },
      }),
      { status: 404 },
    );
  });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("TuningShell", () => {
  it("renders the six section headings in the specified order", async () => {
    installTuningFetchMock();
    renderWithProviders(<TuningShell />);

    // Wait for prompt to load so CriticPromptSection fully renders.
    await screen.findByLabelText("Critic prompt body");

    const headings = screen.getAllByRole("heading", { level: 2 });
    const order = headings.map((h) => h.textContent);
    expect(order).toEqual([
      "Critic Prompt",
      "Fast Feedback",
      "Golden Eval",
      "Shadow Eval",
      "Fixture Management",
      "Calibration",
    ]);
  });

  it("exposes exactly one h1 for the tab", async () => {
    installTuningFetchMock();
    renderWithProviders(<TuningShell />);

    await screen.findByLabelText("Critic prompt body");

    const h1s = screen.getAllByRole("heading", { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveTextContent("Tuning");
  });

  it("shows a save recommendation banner after the prompt is saved", async () => {
    installTuningFetchMock();
    const user = userEvent.setup();
    renderWithProviders(<TuningShell />);

    const editor = await screen.findByLabelText("Critic prompt body");
    await user.clear(editor);
    await user.type(editor, "new content");

    await user.click(screen.getByRole("button", { name: /save prompt/i }));

    const banner = await screen.findByRole("status", {
      name: /save recommendation/i,
    });
    expect(banner).toHaveTextContent(/Prompt saved as/);
  });

  it("keeps Shadow disabled until Golden passes in the current session", async () => {
    installTuningFetchMock();
    const user = userEvent.setup();
    renderWithProviders(<TuningShell />);

    await screen.findByLabelText("Critic prompt body");

    const shadowHeading = screen.getByRole("heading", { name: "Shadow Eval" });
    const shadowSection = shadowHeading.closest("section") as HTMLElement;
    const shadowButton = within(shadowSection).getByRole("button", {
      name: /run shadow eval/i,
    });
    expect(shadowButton).toBeDisabled();
    expect(
      within(shadowSection).getByText(
        /Golden must pass this session before Shadow can run/i,
      ),
    ).toBeInTheDocument();

    // Kick Golden off — it returns false_rejects=0, so the gate opens.
    const goldenHeading = screen.getByRole("heading", { name: "Golden Eval" });
    const goldenSection = goldenHeading.closest("section") as HTMLElement;
    await user.click(
      within(goldenSection).getByRole("button", { name: /run golden eval/i }),
    );

    await waitFor(() => {
      expect(shadowButton).not.toBeDisabled();
    });
  });

  it("leaves Shadow disabled when Golden reports false rejections", async () => {
    installTuningFetchMock({ goldenFalseRejects: 2 });
    const user = userEvent.setup();
    renderWithProviders(<TuningShell />);

    await screen.findByLabelText("Critic prompt body");

    const goldenHeading = screen.getByRole("heading", { name: "Golden Eval" });
    const goldenSection = goldenHeading.closest("section") as HTMLElement;
    await user.click(
      within(goldenSection).getByRole("button", { name: /run golden eval/i }),
    );

    // Wait for the Golden report row so we know the mutation settled.
    await within(goldenSection).findByText(/false rejects/i);

    const shadowHeading = screen.getByRole("heading", { name: "Shadow Eval" });
    const shadowSection = shadowHeading.closest("section") as HTMLElement;
    const shadowButton = within(shadowSection).getByRole("button", {
      name: /run shadow eval/i,
    });
    expect(shadowButton).toBeDisabled();
  });
});
