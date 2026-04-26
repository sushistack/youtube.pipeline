import { expect, test } from "./fixtures";

test("UI-E2E-07 tuning flow surfaces DeepSeek critic and unlocks Shadow after Golden", async ({
  page,
  skipOnboarding,
  consoleGuard,
}) => {
  void skipOnboarding;

  // Spy on the prompt-save PUT to mimic the prompt_versions DB row check
  // from the test design (AC-1). Only the PUT body is captured; GET reads
  // remain untouched.
  const promptSavePayloads: string[] = [];
  await page.route("**/api/tuning/critic-prompt", async (route) => {
    const request = route.request();
    if (request.method() === "GET") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          version: 1,
          data: {
            body: "# Critic prompt\n\nbaseline.\n",
            saved_at: "2026-04-25T00:00:00Z",
            prompt_hash: "abc123",
            git_short_sha: "abc1234",
            version_tag: "20260425T000000Z-abc1234",
          },
        }),
      });
    }
    if (request.method() === "PUT") {
      promptSavePayloads.push(request.postData() ?? "");
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        version: 1,
        data: {
          body: "# Critic prompt\n\nsaved.\n",
          saved_at: "2026-04-25T00:01:00Z",
          prompt_hash: "def456",
          git_short_sha: "abc1234",
          version_tag: "20260425T000100Z-abc1234",
        },
      }),
    });
  });

  await page.route("**/api/tuning/golden", async (route) => {
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        version: 1,
        data: {
          pairs: [],
          pair_count: 1,
          freshness: {
            warnings: [],
            days_since_refresh: 0,
            prompt_hash_changed: false,
            current_prompt_hash: "abc123",
          },
          last_report: null,
        },
      }),
    });
  });

  await page.route("**/api/tuning/golden/run", async (route) => {
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        version: 1,
        data: {
          recall: 1,
          total_negative: 2,
          detected_negative: 2,
          false_rejects: 0,
        },
      }),
    });
  });

  await page.route("**/api/tuning/shadow/run", async (route) => {
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        version: 1,
        data: {
          window: 20,
          evaluated: 1,
          false_rejections: 0,
          empty: false,
          summary_line: "shadow eval: window=20 evaluated=1 false_rejections=0",
          critic_provider: "deepseek",
          critic_model: "deepseek-chat",
          version_tag: "20260425T000100Z-abc1234",
          results: [
            {
              run_id: "scp-049-run-1",
              created_at: "2026-04-25T00:00:00Z",
              baseline_verdict: "pass",
              baseline_score: 0.91,
              new_verdict: "pass",
              new_retry_reason: "",
              new_overall_score: 92,
              new_critic_provider: "deepseek",
              new_critic_model: "deepseek-chat",
              overall_diff: 0.01,
              false_rejection: false,
            },
          ],
        },
      }),
    });
  });

  await page.route("**/api/tuning/fast-feedback", async (route) => {
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        version: 1,
        data: {
          sample_count: 10,
          pass_count: 10,
          retry_count: 0,
          accept_with_notes_count: 0,
          duration_ms: 125,
          critic_provider: "deepseek",
          critic_model: "deepseek-chat",
          version_tag: "20260425T000100Z-abc1234",
          samples: [],
        },
      }),
    });
  });

  await page.route("**/api/tuning/calibration**", async (route) => {
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        version: 1,
        data: {
          window: 20,
          limit: 30,
          points: [],
          latest: null,
        },
      }),
    });
  });

  await page.goto("/tuning");

  // Step 1: edit + save Critic prompt. Save button is disabled until the
  // textarea content diverges from the loaded baseline; typing the v2
  // variant flips it. Mock spy then verifies the PUT actually fired.
  const promptEditor = page.getByRole("textbox", { name: "Critic prompt body" });
  await promptEditor.fill("# Critic prompt\n\nv2 variant.\n");
  const saveButton = page.getByRole("button", { name: "Save prompt" });
  await expect(saveButton).toBeEnabled();
  await saveButton.click();
  await expect(
    page.getByText(/Saved as 20260425T000100Z-abc1234/),
  ).toBeVisible();
  expect(promptSavePayloads).toHaveLength(1);
  expect(promptSavePayloads[0]).toContain("v2 variant");

  const shadowButton = page.getByRole("button", { name: "Run Shadow eval" });
  await expect(shadowButton).toBeDisabled();

  // Step 2: Golden run displays recall=100% and unlocks Shadow (AC-6 gate).
  await page.getByRole("button", { name: "Run Golden eval" }).click();
  const goldenRegion = page.getByRole("region", { name: /Golden Eval/i });
  await expect(goldenRegion.getByText(/recall 100\.0%/)).toBeVisible();
  await expect(goldenRegion.getByText(/0 false rejects/)).toBeVisible();
  await expect(shadowButton).toBeEnabled();

  await shadowButton.click();

  // Both the Shadow and Fast Feedback sections render <code>deepseek</code>
  // once the run completes. Scoping to the Shadow result region avoids
  // strict-mode "resolved to N elements" failures and keeps the assertion
  // anchored to the section under test.
  const shadowRegion = page.getByRole("region", { name: /Shadow/i }).first();
  await expect(shadowRegion.getByText("Critic runtime")).toBeVisible();
  await expect(shadowRegion.getByText("deepseek", { exact: true }).first()).toBeVisible();
  await expect(shadowRegion.getByText("deepseek-chat", { exact: true }).first()).toBeVisible();
  await expect(shadowRegion.getByText(/No regressions detected/i)).toBeVisible();

  // Step 4 + 5: per-run drilldown. The diff list serializes the verdict and
  // overall_diff for each replayed run; the test design's "click scene 1"
  // step maps to the per-run row in the current report shape.
  const shadowRow = shadowRegion.getByRole("listitem").first();
  await expect(shadowRow.getByText("scp-049-run-1")).toBeVisible();
  await expect(shadowRow.getByText("pass", { exact: true })).toBeVisible();
  await expect(shadowRow.getByText(/diff 0\.01/)).toBeVisible();

  expect(consoleGuard.consoleErrors).toEqual([]);
  expect(consoleGuard.pageErrors).toEqual([]);
});
