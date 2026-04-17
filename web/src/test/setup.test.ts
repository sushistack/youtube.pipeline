import { describe, it, expect } from "vitest";

describe("external fetch blocking", () => {
  it("rejects external URLs", async () => {
    await expect(fetch("https://example.com/api")).rejects.toThrow(
      "external fetch blocked in test: https://example.com/api",
    );
  });

  it("rejects external HTTP URLs", async () => {
    await expect(fetch("http://api.openai.com/v1/chat")).rejects.toThrow(
      "external fetch blocked in test:",
    );
  });

  it("allows localhost URLs", async () => {
    // We can't actually connect, but the fetch should NOT be blocked —
    // it should fail with a network error, not our blocker message.
    try {
      await fetch("http://localhost:3000/api/health");
    } catch (e) {
      const msg = (e as Error).message;
      expect(msg).not.toContain("external fetch blocked in test:");
    }
  });

  it("allows 127.0.0.1 URLs", async () => {
    try {
      await fetch("http://127.0.0.1:3000/api/health");
    } catch (e) {
      const msg = (e as Error).message;
      expect(msg).not.toContain("external fetch blocked in test:");
    }
  });
});
