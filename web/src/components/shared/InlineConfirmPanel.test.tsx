import "@testing-library/jest-dom";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { InlineConfirmPanel } from "./InlineConfirmPanel";

describe("InlineConfirmPanel", () => {
  it("renders as a modal alertdialog and autofocuses the confirm button", () => {
    render(
      <InlineConfirmPanel
        confirm_label="This will approve"
        count={3}
        on_cancel={vi.fn()}
        on_confirm={vi.fn()}
      />,
    );

    const panel = screen.getByRole("alertdialog");
    expect(panel).toBeInTheDocument();
    expect(panel).toHaveAttribute("aria-modal", "true");
    expect(panel).toHaveAttribute("aria-labelledby");
    expect(panel).toHaveAttribute("aria-describedby");
    expect(
      screen.getByText(/this will approve 3 remaining scenes/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /\[enter\] confirm/i }),
    ).toHaveFocus();
  });

  it("traps focus in both directions (forward and reverse tab)", async () => {
    const user = userEvent.setup();

    render(
      <InlineConfirmPanel
        confirm_label="This will approve"
        count={2}
        on_cancel={vi.fn()}
        on_confirm={vi.fn()}
      />,
    );

    const confirm = screen.getByRole("button", { name: /\[enter\] confirm/i });
    const cancel = screen.getByRole("button", { name: /\[esc\] cancel/i });

    expect(confirm).toHaveFocus();

    // Forward tab: confirm → cancel
    await user.tab();
    expect(cancel).toHaveFocus();

    // Forward tab again: wrap cancel → confirm (AC-1 forward-tab trap)
    await user.tab();
    expect(confirm).toHaveFocus();

    // Reverse tab: confirm → cancel (wrap via shift+tab)
    await user.tab({ shift: true });
    expect(cancel).toHaveFocus();

    // Reverse tab: cancel → confirm
    await user.tab({ shift: true });
    expect(confirm).toHaveFocus();
  });

  it("Enter on the Confirm button fires on_confirm", async () => {
    const user = userEvent.setup();
    const on_confirm = vi.fn();
    const on_cancel = vi.fn();

    render(
      <InlineConfirmPanel
        confirm_label="This will approve"
        count={2}
        on_cancel={on_cancel}
        on_confirm={on_confirm}
      />,
    );

    await user.keyboard("{Enter}");
    expect(on_confirm).toHaveBeenCalledTimes(1);
    expect(on_cancel).not.toHaveBeenCalled();
  });

  it("Enter on the Cancel button fires on_cancel, not on_confirm", async () => {
    const user = userEvent.setup();
    const on_confirm = vi.fn();
    const on_cancel = vi.fn();

    render(
      <InlineConfirmPanel
        confirm_label="This will approve"
        count={2}
        on_cancel={on_cancel}
        on_confirm={on_confirm}
      />,
    );

    const cancel = screen.getByRole("button", { name: /\[esc\] cancel/i });
    cancel.focus();
    await user.keyboard("{Enter}");

    expect(on_cancel).toHaveBeenCalledTimes(1);
    expect(on_confirm).not.toHaveBeenCalled();
  });

  it("Esc fires on_cancel", async () => {
    const user = userEvent.setup();
    const on_cancel = vi.fn();

    render(
      <InlineConfirmPanel
        confirm_label="This will approve"
        count={2}
        on_cancel={on_cancel}
        on_confirm={vi.fn()}
      />,
    );

    await user.keyboard("{Escape}");
    expect(on_cancel).toHaveBeenCalledTimes(1);
  });

  it("ignores Enter and Esc while is_confirming to prevent double-submit", async () => {
    const user = userEvent.setup();
    const on_confirm = vi.fn();
    const on_cancel = vi.fn();

    render(
      <InlineConfirmPanel
        confirm_label="This will approve"
        count={2}
        is_confirming
        on_cancel={on_cancel}
        on_confirm={on_confirm}
      />,
    );

    await user.keyboard("{Enter}");
    await user.keyboard("{Escape}");

    expect(on_confirm).not.toHaveBeenCalled();
    expect(on_cancel).not.toHaveBeenCalled();
  });
});
