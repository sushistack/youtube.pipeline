import "@testing-library/jest-dom";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { KeyboardShortcutsProvider } from "../../hooks/useKeyboardShortcuts";
import { renderWithProviders } from "../../test/renderWithProviders";
import { BatchReview } from "./BatchReview";
import type { RunSummary } from "../../contracts/runContracts";
import { useUIStore } from "../../stores/useUIStore";

function requestUrl(input: string | URL | Request) {
  if (typeof input === "string") {
    return input;
  }
  if (input instanceof URL) {
    return input.href;
  }
  return input.url;
}

const run: RunSummary = {
  cost_usd: 1.25,
  created_at: "2026-04-19T00:00:00Z",
  duration_ms: 45000,
  human_override: false,
  id: "scp-049-run-1",
  retry_count: 0,
  scp_id: "049",
  stage: "batch_review",
  status: "waiting",
  token_in: 1200,
  token_out: 400,
  updated_at: "2026-04-19T00:05:00Z",
};

const responsePayload = {
  data: {
    items: [
      {
        clip_path: null,
        content_flags: ["Safeguard Triggered: Minors"],
        critic_breakdown: null,
        critic_score: 55,
        high_leverage: false,
        high_leverage_reason: null,
        high_leverage_reason_code: null,
        narration: "Standard waiting scene",
        previous_version: null,
        review_status: "waiting_for_review",
        scene_index: 1,
        shots: [
          {
            image_path: "/scene-1.png",
            duration_s: 4,
            transition: "cut",
            visual_descriptor: "one",
          },
        ],
        tts_duration_ms: null,
        tts_path: null,
      },
      {
        clip_path: null,
        content_flags: [],
        critic_breakdown: null,
        critic_score: 89,
        high_leverage: true,
        high_leverage_reason: "Opening hook scene",
        high_leverage_reason_code: "hook_scene",
        narration: "Hook scene narration",
        previous_version: null,
        review_status: "waiting_for_review",
        scene_index: 0,
        shots: [
          {
            image_path: "/scene-0.png",
            duration_s: 4,
            transition: "cut",
            visual_descriptor: "zero",
          },
        ],
        tts_duration_ms: 4200,
        tts_path: "/audio/scene-0.wav",
      },
      {
        clip_path: null,
        content_flags: [],
        critic_breakdown: null,
        critic_score: 92,
        high_leverage: true,
        high_leverage_reason: "Act boundary: act_2",
        high_leverage_reason_code: "act_boundary",
        narration: "Approved scene",
        previous_version: null,
        review_status: "approved",
        scene_index: 2,
        shots: [
          {
            image_path: "/scene-2.png",
            duration_s: 4,
            transition: "cut",
            visual_descriptor: "two",
          },
        ],
        tts_duration_ms: null,
        tts_path: null,
      },
    ],
    total: 3,
  },
  version: 1,
};

describe("BatchReview", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
    useUIStore.getState().clear_undo_stack(run.id);
  });

  it("renders both panes and selects the first actionable scene by default", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(responsePayload), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    expect(
      await screen.findByLabelText(/batch review layout/i),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/review scene queue/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/scene 1 detail/i)).toBeInTheDocument();
    expect(screen.getByText("2 scenes remaining")).toBeInTheDocument();
    expect(screen.getAllByText("Hook scene narration")).toHaveLength(2);
  });

  it("keeps selection and detail synchronized with J/K navigation and bounds edges", async () => {
    const user = userEvent.setup();
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(responsePayload), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("j");
    expect(screen.getByLabelText(/scene 2 detail/i)).toBeInTheDocument();

    await user.keyboard("j");
    expect(screen.getByLabelText(/scene 3 detail/i)).toBeInTheDocument();

    await user.keyboard("j");
    await waitFor(() => {
      expect(screen.getByLabelText(/scene 3 detail/i)).toBeInTheDocument();
    });

    await user.keyboard("k");
    expect(screen.getByLabelText(/scene 2 detail/i)).toBeInTheDocument();

    await user.keyboard("k");
    expect(screen.getByLabelText(/scene 1 detail/i)).toBeInTheDocument();

    await user.keyboard("k");
    await waitFor(() => {
      expect(screen.getByLabelText(/scene 1 detail/i)).toBeInTheDocument();
    });
  });

  it("approves the selected scene, moves forward, and updates the visible remaining count", async () => {
    const user = userEvent.setup();
    const fetch_mock = vi.spyOn(globalThis, "fetch");
    let approved = false;
    fetch_mock.mockImplementation(async (input, init) => {
      const url = requestUrl(input);
      if (
        url.endsWith("/review-items") &&
        (!init?.method || init.method === "GET")
      ) {
        const payload = approved
          ? {
              ...responsePayload,
              data: {
                ...responsePayload.data,
                items: responsePayload.data.items.map((item) =>
                  item.scene_index === 0
                    ? { ...item, review_status: "approved" }
                    : item,
                ),
              },
            }
          : responsePayload;
        return new Response(JSON.stringify(payload), {
          headers: { "Content-Type": "application/json" },
          status: 200,
        });
      }
      if (url.endsWith("/decisions")) {
        approved = true;
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              scene_index: 0,
              decision_type: "approve",
              next_scene_index: 1,
            },
          }),
          {
            headers: { "Content-Type": "application/json" },
            status: 200,
          },
        );
      }
      if (url.endsWith("/status")) {
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              decisions_summary: {
                approved_count: 2,
                pending_count: 1,
                rejected_count: 0,
              },
              run,
            },
          }),
          {
            headers: { "Content-Type": "application/json" },
            status: 200,
          },
        );
      }
      throw new Error(`Unexpected fetch ${url}`);
    });

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("{Enter}");

    await waitFor(() => {
      expect(screen.getByLabelText(/scene 2 detail/i)).toBeInTheDocument();
    });
    expect(screen.getByText("1 scenes remaining")).toBeInTheDocument();
  });

  it("Shift+Enter opens the inline approve-all confirmation panel with alertdialog semantics", async () => {
    const user = userEvent.setup();
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(responsePayload), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("{Shift>}{Enter}{/Shift}");

    const panel = await screen.findByRole("alertdialog");
    expect(panel).toBeInTheDocument();
    expect(
      screen.getByText(/this will approve 2 remaining scenes in this run/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /\[enter\] confirm/i }),
    ).toHaveFocus();
  });

  it("approve-all panel traps focus and Esc restores focus to the invoking trigger", async () => {
    const user = userEvent.setup();
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(responsePayload), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    const trigger = screen.getByRole("button", {
      name: /shift\+enter.*approve all remaining/i,
    });
    await user.click(trigger);

    const confirm = await screen.findByRole("button", {
      name: /\[enter\] confirm/i,
    });
    const cancel = screen.getByRole("button", { name: /\[esc\] cancel/i });
    expect(confirm).toHaveFocus();

    await user.tab();
    expect(cancel).toHaveFocus();

    await user.tab();
    expect(confirm).toHaveFocus();

    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    });
    expect(trigger).toHaveFocus();
  });

  it("confirming approve-all calls the batch endpoint and pushes one aggregate undo entry", async () => {
    const user = userEvent.setup();
    const fetch_mock = vi.spyOn(globalThis, "fetch");
    let approved_all = false;
    fetch_mock.mockImplementation(async (input, init) => {
      const url = requestUrl(input);
      if (
        url.endsWith("/review-items") &&
        (!init?.method || init.method === "GET")
      ) {
        const payload = approved_all
          ? {
              ...responsePayload,
              data: {
                ...responsePayload.data,
                items: responsePayload.data.items.map((item) =>
                  item.review_status === "waiting_for_review"
                    ? { ...item, review_status: "approved" }
                    : item,
                ),
              },
            }
          : responsePayload;
        return new Response(JSON.stringify(payload), {
          headers: { "Content-Type": "application/json" },
          status: 200,
        });
      }
      if (url.endsWith("/approve-all-remaining") && init?.method === "POST") {
        approved_all = true;
        expect(JSON.parse(String(init.body))).toEqual({ focus_scene_index: 0 });
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              aggregate_command_id: "batch-approve-1",
              approved_count: 2,
              approved_scene_indices: [0, 1],
              focus_scene_index: 0,
            },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      if (url.endsWith("/status")) {
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              decisions_summary: {
                approved_count: 3,
                pending_count: 0,
                rejected_count: 0,
              },
              run,
            },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      if (url.endsWith("/undo")) {
        approved_all = false;
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              undone_scene_index: 0,
              undone_kind: "approve_all_remaining",
              focus_target: "scene-card",
            },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      throw new Error(`Unexpected fetch ${url}`);
    });

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("{Shift>}{Enter}{/Shift}");
    await user.keyboard("{Enter}");

    await waitFor(() => {
      expect(screen.getByText("All scenes reviewed")).toBeInTheDocument();
    });

    const undoStack = useUIStore.getState().undo_stacks[run.id];
    expect(undoStack).toHaveLength(1);
    expect(undoStack[0].kind).toBe("approve_all_remaining");
    expect(undoStack[0].scene_indices).toEqual([0, 1]);

    await user.keyboard("{Control>}z{/Control}");
    const undo_calls = fetch_mock.mock.calls.filter(([input]) =>
      requestUrl(input).endsWith("/undo"),
    );
    expect(undo_calls).toHaveLength(1);
  });

  it("suppresses action shortcuts while focus is inside an input", async () => {
    const user = userEvent.setup();
    const fetch_mock = vi.spyOn(globalThis, "fetch");
    fetch_mock.mockResolvedValue(
      new Response(JSON.stringify(responsePayload), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <div>
          <input aria-label="edit field" />
          <BatchReview run={run} />
        </div>
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.click(screen.getByLabelText(/edit field/i));
    await user.keyboard("{Enter}");

    expect(fetch_mock).toHaveBeenCalledTimes(1);
  });

  it("uses space for audio without triggering approve or reject handlers", async () => {
    const user = userEvent.setup();
    const fetch_mock = vi.spyOn(globalThis, "fetch");
    fetch_mock.mockImplementation(async (input) => {
      const url = requestUrl(input);
      if (url.endsWith("/review-items")) {
        return new Response(JSON.stringify(responsePayload), {
          headers: { "Content-Type": "application/json" },
          status: 200,
        });
      }
      throw new Error(`Unexpected fetch ${url}`);
    });

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard(" ");

    expect(HTMLMediaElement.prototype.play).toHaveBeenCalled();
    expect(fetch_mock).toHaveBeenCalledTimes(1);
  });

  it("Ctrl+Z fires undo endpoint after an approval and restores scene selection", async () => {
    const user = userEvent.setup();
    const fetch_mock = vi.spyOn(globalThis, "fetch");
    let approved = false;
    fetch_mock.mockImplementation(async (input) => {
      const url = requestUrl(input);
      if (url.endsWith("/review-items")) {
        const payload = approved
          ? {
              ...responsePayload,
              data: {
                ...responsePayload.data,
                items: responsePayload.data.items.map((item) =>
                  item.scene_index === 0
                    ? { ...item, review_status: "approved" }
                    : item,
                ),
              },
            }
          : responsePayload;
        return new Response(JSON.stringify(payload), {
          headers: { "Content-Type": "application/json" },
          status: 200,
        });
      }
      if (url.endsWith("/decisions")) {
        approved = true;
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              scene_index: 0,
              decision_type: "approve",
              next_scene_index: 1,
            },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      if (url.endsWith("/undo")) {
        approved = false;
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              undone_scene_index: 0,
              undone_kind: "approve",
              focus_target: "scene-card",
            },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      if (url.endsWith("/status")) {
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              run,
              decisions_summary: {
                approved_count: 1,
                pending_count: 1,
                rejected_count: 0,
              },
            },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      throw new Error(`Unexpected fetch: ${url}`);
    });

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("{Enter}");

    await waitFor(() => {
      expect(screen.getByLabelText(/scene 2 detail/i)).toBeInTheDocument();
    });

    // Ctrl+Z should undo the approval and restore selection to scene 0 (scene 1 in 1-indexed label).
    await user.keyboard("{Control>}z{/Control}");

    await waitFor(() => {
      expect(screen.getByLabelText(/scene 1 detail/i)).toBeInTheDocument();
    });

    const undo_calls = fetch_mock.mock.calls.filter(([input]) =>
      requestUrl(input).endsWith("/undo"),
    );
    expect(undo_calls).toHaveLength(1);
  });

  it("Ctrl+Z is suppressed when focus is inside a textarea", async () => {
    const user = userEvent.setup();
    const fetch_mock = vi.spyOn(globalThis, "fetch");
    fetch_mock.mockResolvedValue(
      new Response(JSON.stringify(responsePayload), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <div>
          <textarea aria-label="draft area" />
          <BatchReview run={run} />
        </div>
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.click(screen.getByLabelText(/draft area/i));
    await user.keyboard("{Control>}z{/Control}");

    // Only the initial review-items fetch should fire, no /undo call.
    const undo_calls = fetch_mock.mock.calls.filter(([input]) =>
      requestUrl(input).endsWith("/undo"),
    );
    expect(undo_calls).toHaveLength(0);
  });

  it("Undo button is disabled when undo stack is empty", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(responsePayload), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    const undo_btn = screen.getByRole("button", { name: /ctrl\+z.*undo/i });
    expect(undo_btn).toBeDisabled();
  });

  // ── Story 8.4 — inline reject composer, FR53, regen, retry-exhausted ─────

  it("Esc opens the inline composer without firing an immediate reject mutation", async () => {
    const user = userEvent.setup();
    const fetch_mock = vi.spyOn(globalThis, "fetch");
    fetch_mock.mockImplementation(async (input) => {
      const url = requestUrl(input);
      if (url.endsWith("/review-items")) {
        return new Response(JSON.stringify(responsePayload), {
          headers: { "Content-Type": "application/json" },
          status: 200,
        });
      }
      throw new Error(`Unexpected fetch: ${url}`);
    });

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("{Escape}");

    // Composer appears inline; never renders a dialog role.
    expect(
      await screen.findByLabelText(/reject composer for scene 1/i),
    ).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    // Focus lands in the reason textarea.
    expect(screen.getByLabelText(/rejection reason/i)).toHaveFocus();
    // No reject fetch fired yet — only the initial review-items GET.
    const decision_calls = fetch_mock.mock.calls.filter(
      ([input, init]) =>
        requestUrl(input).endsWith("/decisions") && init?.method === "POST",
    );
    expect(decision_calls).toHaveLength(0);
  });

  it("blocks submission when the reason is empty and shows inline validation", async () => {
    const user = userEvent.setup();
    const fetch_mock = vi.spyOn(globalThis, "fetch");
    fetch_mock.mockResolvedValue(
      new Response(JSON.stringify(responsePayload), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("{Escape}");
    const confirm = await screen.findByRole("button", {
      name: /confirm reject/i,
    });
    await user.click(confirm);

    expect(
      await screen.findByText(/rejection reason is required/i),
    ).toBeInTheDocument();
    const decision_calls = fetch_mock.mock.calls.filter(
      ([input, init]) =>
        requestUrl(input).endsWith("/decisions") && init?.method === "POST",
    );
    expect(decision_calls).toHaveLength(0);
  });

  it("Esc with an empty composer closes it and restores focus", async () => {
    const user = userEvent.setup();
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(responsePayload), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("{Escape}");
    expect(
      await screen.findByLabelText(/reject composer for scene 1/i),
    ).toBeInTheDocument();

    // Esc inside an empty textarea cancels the composer state.
    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(
        screen.queryByLabelText(/reject composer for scene 1/i),
      ).not.toBeInTheDocument();
    });
  });

  it("confirms reject with note, surfaces FR53 warning, and dispatches regeneration", async () => {
    const user = userEvent.setup();
    const fetch_mock = vi.spyOn(globalThis, "fetch");
    let regen_dispatched = false;
    fetch_mock.mockImplementation(async (input, init) => {
      const url = requestUrl(input);
      if (
        url.endsWith("/review-items") &&
        (!init?.method || init.method === "GET")
      ) {
        return new Response(JSON.stringify(responsePayload), {
          headers: { "Content-Type": "application/json" },
          status: 200,
        });
      }
      if (url.endsWith("/decisions") && init?.method === "POST") {
        const body = JSON.parse(String(init?.body ?? "{}"));
        expect(body.note).toBe("tone is off");
        expect(body.decision_type).toBe("reject");
        expect(body.scene_index).toBe(0);
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              scene_index: 0,
              decision_type: "reject",
              next_scene_index: 1,
              regen_attempts: 1,
              retry_exhausted: false,
              prior_rejection: {
                run_id: "prior-run-a",
                scp_id: "049",
                scene_index: 0,
                reason: "cadence off in the prior run",
                created_at: "2026-03-12T09:30:00Z",
              },
            },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      if (url.endsWith("/scenes/0/regen") && init?.method === "POST") {
        regen_dispatched = true;
        return new Response(
          JSON.stringify({
            version: 1,
            data: { scene_index: 0, regen_attempts: 1, retry_exhausted: false },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      if (url.endsWith("/status")) {
        return new Response(JSON.stringify({ version: 1, data: { run } }), {
          headers: { "Content-Type": "application/json" },
          status: 200,
        });
      }
      throw new Error(`Unexpected fetch: ${url}`);
    });

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("{Escape}");
    const textarea = await screen.findByLabelText(/rejection reason/i);
    await user.type(textarea, "tone is off");
    const confirm = screen.getByRole("button", { name: /confirm reject/i });
    await user.click(confirm);

    await waitFor(() => {
      expect(regen_dispatched).toBe(true);
    });

    // Composer closes after a successful submit so the operator can keep reviewing.
    await waitFor(() => {
      expect(
        screen.queryByLabelText(/reject composer for scene 1/i),
      ).not.toBeInTheDocument();
    });
  });

  it("shows the FR53 warning inside the composer when review-items includes a prior rejection", async () => {
    const user = userEvent.setup();
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = requestUrl(input);
      if (url.endsWith("/review-items")) {
        const items = responsePayload.data.items.map((item) =>
          item.scene_index === 0
            ? {
                ...item,
                prior_rejection: {
                  run_id: "prior-run-a",
                  scp_id: "049",
                  scene_index: 0,
                  reason: "cadence off in the prior run",
                  created_at: "2026-03-12T09:30:00Z",
                },
              }
            : item,
        );
        return new Response(
          JSON.stringify({
            ...responsePayload,
            data: { ...responsePayload.data, items },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      throw new Error(`Unexpected fetch: ${url}`);
    });

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("{Escape}");
    expect(
      await screen.findByText(/we've seen this scene fail before/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/cadence off in the prior run/i),
    ).toBeInTheDocument();
  });

  it("shows retry-exhausted CTAs when the scene has reached the cap", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = requestUrl(input);
      if (url.endsWith("/review-items")) {
        const items = responsePayload.data.items.map((item) => {
          if (item.scene_index !== 0) return item;
          return { ...item, regen_attempts: 2, retry_exhausted: true };
        });
        return new Response(
          JSON.stringify({
            ...responsePayload,
            data: { ...responsePayload.data, items },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      throw new Error(`Unexpected fetch: ${url}`);
    });

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    expect(await screen.findByText(/retry limit reached/i)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /manual edit/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /skip & flag/i }),
    ).toBeInTheDocument();
    // Normal reject/approve buttons must not render alongside the exhausted CTAs.
    expect(
      screen.queryByRole("button", { name: /\[enter\] approve/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /\[esc\] reject/i }),
    ).not.toBeInTheDocument();
  });

  it("skip & flag records a skip_and_remember with retry-exhausted context", async () => {
    const user = userEvent.setup();
    const fetch_mock = vi.spyOn(globalThis, "fetch");
    let recorded_payload: unknown = null;
    fetch_mock.mockImplementation(async (input, init) => {
      const url = requestUrl(input);
      if (url.endsWith("/review-items")) {
        const items = responsePayload.data.items.map((item) =>
          item.scene_index === 0
            ? { ...item, regen_attempts: 2, retry_exhausted: true }
            : item,
        );
        return new Response(
          JSON.stringify({
            ...responsePayload,
            data: { ...responsePayload.data, items },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      if (url.endsWith("/decisions") && init?.method === "POST") {
        recorded_payload = JSON.parse(String(init?.body ?? "{}"));
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              scene_index: 0,
              decision_type: "skip_and_remember",
              next_scene_index: 1,
              regen_attempts: 2,
              retry_exhausted: true,
            },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      if (url.endsWith("/status")) {
        return new Response(JSON.stringify({ version: 1, data: { run } }), {
          headers: { "Content-Type": "application/json" },
          status: 200,
        });
      }
      throw new Error(`Unexpected fetch: ${url}`);
    });

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.click(
      await screen.findByRole("button", { name: /skip & flag/i }),
    );

    await waitFor(() => {
      expect(recorded_payload).not.toBeNull();
    });
    const payload = recorded_payload as {
      decision_type?: string;
      context_snapshot?: { flagged?: boolean; flag_reason?: string };
    };
    expect(payload.decision_type).toBe("skip_and_remember");
    expect(payload.context_snapshot?.flagged).toBe(true);
    expect(payload.context_snapshot?.flag_reason).toBe("retry_exhausted");
  });

  it("keeps other scenes reviewable while a reject is regenerating", async () => {
    const user = userEvent.setup();
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = requestUrl(input);
      if (url.endsWith("/review-items")) {
        return new Response(JSON.stringify(responsePayload), {
          headers: { "Content-Type": "application/json" },
          status: 200,
        });
      }
      if (url.endsWith("/decisions") && init?.method === "POST") {
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              scene_index: 0,
              decision_type: "reject",
              next_scene_index: 1,
              regen_attempts: 1,
              retry_exhausted: false,
            },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      if (url.endsWith("/scenes/0/regen")) {
        return new Response(
          JSON.stringify({
            version: 1,
            data: { scene_index: 0, regen_attempts: 1, retry_exhausted: false },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      if (url.endsWith("/status")) {
        return new Response(JSON.stringify({ version: 1, data: { run } }), {
          headers: { "Content-Type": "application/json" },
          status: 200,
        });
      }
      throw new Error(`Unexpected fetch: ${url}`);
    });

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("{Escape}");
    await user.type(screen.getByLabelText(/rejection reason/i), "rephrase");
    await user.click(screen.getByRole("button", { name: /confirm reject/i }));

    // List still renders the other scenes and J/K still navigates.
    await waitFor(() => {
      expect(
        screen.queryByLabelText(/reject composer for scene 1/i),
      ).not.toBeInTheDocument();
    });
    await user.keyboard("j");
    expect(screen.getByLabelText(/scene 2 detail/i)).toBeInTheDocument();
  });

  it("Shift+Enter does not open the confirmation panel when zero scenes are actionable (AC-1)", async () => {
    const user = userEvent.setup();
    const allApprovedPayload = {
      ...responsePayload,
      data: {
        ...responsePayload.data,
        items: responsePayload.data.items.map((item) => ({
          ...item,
          review_status: "approved" as const,
        })),
      },
    };
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(allApprovedPayload), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByText(/all scenes reviewed for this run/i);
    // The trigger is not rendered when there is no selected actionable item;
    // the action bar collapses to the "all reviewed" empty state.
    expect(
      screen.queryByRole("button", {
        name: /shift\+enter.*approve all remaining/i,
      }),
    ).not.toBeInTheDocument();

    await user.keyboard("{Shift>}{Enter}{/Shift}");
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("skips the phantom undo entry when the server approves zero scenes", async () => {
    // Race: UI cache shows actionable scenes but the server sees zero
    // by the time the POST lands. The server returns an empty aggregate id
    // and the client MUST NOT push an undo entry that maps to nothing.
    const user = userEvent.setup();
    const fetch_mock = vi.spyOn(globalThis, "fetch");
    fetch_mock.mockImplementation(async (input, init) => {
      const url = requestUrl(input);
      if (
        url.endsWith("/review-items") &&
        (!init?.method || init.method === "GET")
      ) {
        return new Response(JSON.stringify(responsePayload), {
          headers: { "Content-Type": "application/json" },
          status: 200,
        });
      }
      if (url.endsWith("/approve-all-remaining") && init?.method === "POST") {
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              aggregate_command_id: "",
              approved_count: 0,
              approved_scene_indices: [],
              focus_scene_index: 0,
            },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      if (url.endsWith("/status")) {
        return new Response(
          JSON.stringify({
            version: 1,
            data: {
              decisions_summary: {
                approved_count: 3,
                pending_count: 0,
                rejected_count: 0,
              },
              run,
            },
          }),
          { headers: { "Content-Type": "application/json" }, status: 200 },
        );
      }
      throw new Error(`Unexpected fetch ${url}`);
    });

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <BatchReview run={run} />
      </KeyboardShortcutsProvider>,
    );

    await screen.findByLabelText(/scene 1 detail/i);
    await user.keyboard("{Shift>}{Enter}{/Shift}");
    await user.keyboard("{Enter}");

    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    });

    const undoStack = useUIStore.getState().undo_stacks[run.id] ?? [];
    expect(undoStack).toHaveLength(0);
  });
});
