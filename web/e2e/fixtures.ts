import { test as base, type Page } from "@playwright/test";

const UI_STORE_KEY = "youtube-pipeline-ui";

function seedPersistedUIStore(onboarding_dismissed: boolean) {
  // Matches the zustand persist envelope: { state: {...partial}, version }.
  // Mirrors partialize() in src/stores/useUIStore.ts; undo_stacks intentionally
  // excluded because the store itself does not persist it.
  const payload = {
    state: {
      onboarding_dismissed,
      production_last_seen: {},
      sidebar_collapsed: false,
    },
    version: 0,
  };
  return { key: UI_STORE_KEY, value: JSON.stringify(payload) };
}

type ConsoleGuard = {
  consoleErrors: string[];
  pageErrors: string[];
};

interface PipelineFixtures {
  consoleGuard: ConsoleGuard;
  skipOnboarding: void;
  resetStores: void;
}

export const test = base.extend<PipelineFixtures>({
  // Per-test reset of the zustand localStorage singleton (deferred 6-5
  // singleton-bleed guard). Clears both localStorage and sessionStorage so
  // that production_last_seen / undo_stacks / sidebar_collapsed from a prior
  // test cannot leak into the current page load.
  resetStores: [
    async ({ page }, doneFixture) => {
      await page.addInitScript(() => {
        try {
          window.localStorage.clear();
          window.sessionStorage.clear();
        } catch {
          // storage access may be denied on non-http schemes; tests load
          // http://127.0.0.1 so this is a no-op there.
        }
      });
      await doneFixture();
    },
    { auto: true },
  ],

  // Pre-dismiss onboarding for every test that opts in. UI-E2E-01 does NOT
  // opt in because it asserts the fresh-boot onboarding flow; every other
  // test in this batch uses it to skip the "Continue to workspace" click.
  skipOnboarding: async ({ page }, doneFixture) => {
    const seed = seedPersistedUIStore(true);
    await page.addInitScript(({ key, value }) => {
      try {
        window.localStorage.setItem(key, value);
      } catch {
        // see resetStores note
      }
    }, seed);
    await doneFixture();
  },

  // Auto-install console/pageerror listeners and publish the arrays so each
  // spec can assert zero errors without repeating the boilerplate.
  consoleGuard: [
    async ({ page }, doneFixture) => {
      const consoleErrors: string[] = [];
      const pageErrors: string[] = [];
      page.on("console", (message) => {
        if (message.type() === "error") {
          consoleErrors.push(message.text());
        }
      });
      page.on("pageerror", (error) => {
        pageErrors.push(error.message);
      });
      await doneFixture({ consoleErrors, pageErrors });
    },
    { auto: true },
  ],
});

export { expect } from "@playwright/test";

export async function dismissOnboardingIfVisible(page: Page) {
  const button = page.getByRole("button", { name: "Continue to workspace" });
  if (await button.isVisible().catch(() => false)) {
    await button.click();
  }
}
