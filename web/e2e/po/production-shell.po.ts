import type { Locator, Page } from '@playwright/test'
import { expect } from '@playwright/test'
import { dismissOnboardingIfVisible } from '../fixtures'

export class ProductionShellPO {
  readonly page: Page

  constructor(page: Page) {
    this.page = page
  }

  async goto(runId?: string) {
    const path = runId
      ? `/production?run=${encodeURIComponent(runId)}`
      : '/production'
    await this.page.goto(path)
  }

  async gotoAndDismissOnboarding(runId?: string) {
    await this.goto(runId)
    await dismissOnboardingIfVisible(this.page)
  }

  heading(): Locator {
    return this.page.getByRole('heading', { name: 'Production', level: 1 })
  }

  statusBar(): Locator {
    // StatusBar renders a <section aria-label="Run telemetry"> — falling back
    // to role=status if the real UI uses that instead. getByTestId avoided in
    // favor of a semantic anchor that already exists.
    return this.page.locator('.status-bar').first()
  }

  stageStepper(): Locator {
    // StageStepper renders role="listbox" with aria-label="Pipeline progress: …".
    return this.page.getByRole('listbox').filter({ hasText: /Pipeline/i }).first()
  }

  sidebar(): Locator {
    return this.page.getByRole('complementary', { name: 'Primary' })
  }

  newRunButton(): Locator {
    return this.page.getByRole('button', { name: 'Create a new pipeline run' })
  }

  newRunPanel(): Locator {
    return this.page.getByRole('alertdialog', { name: 'Create a new pipeline run' })
  }

  runInventorySearch(): Locator {
    // <input type="search" placeholder="Search runs"> wrapped in a label
    // whose accessible name is "Search runs".
    return this.page.getByRole('searchbox', { name: 'Search runs' })
  }

  runCard(): Locator {
    // RunCard is role="button" (not an actual <button>) so locate by class.
    return this.page.locator('.run-card')
  }

  runCardFor(scpId: string): Locator {
    return this.runCard().filter({ hasText: `SCP-${scpId}` }).first()
  }

  selectedRunHeading(): Locator {
    // Hero on ProductionShell surfaces the selected run id as an h2.
    return this.page.locator('.production-dashboard__title')
  }

  failureBanner(): Locator {
    return this.page.getByRole('alert', { name: 'Run failure recovery' })
  }

  // Scenario inspector (UI-E2E-02)
  scenarioInspector(): Locator {
    return this.page.getByRole('region', { name: 'Scenario narration review' })
  }

  narrationReadCell(sceneIndex: number): Locator {
    // scene_index is zero-based in state; label is 1-based.
    return this.page.getByRole('button', {
      name: `Narration for scene ${sceneIndex + 1}. Press Tab, Enter, or click to edit.`,
    })
  }

  narrationTextarea(sceneIndex: number): Locator {
    return this.page.getByRole('textbox', {
      name: `Narration for scene ${sceneIndex + 1}`,
    })
  }

  // Character pick (UI-E2E-03)
  characterPick(): Locator {
    return this.page.getByRole('region', { name: 'Character pick' })
  }

  characterGrid(): Locator {
    return this.page.getByRole('listbox', { name: 'Character candidate grid' })
  }

  characterCellByDigit(digit: string): Locator {
    return this.page.getByTestId(`character-grid-cell-${digit}`)
  }

  visionDescriptorTextarea(): Locator {
    return this.page.getByRole('textbox', { name: 'Vision Descriptor draft' })
  }

  visionDescriptorReadCell(): Locator {
    return this.page.getByRole('button', {
      name: /Vision Descriptor draft\. Press Tab to edit or Enter to confirm/i,
    })
  }

  async expectShellReady() {
    await expect(this.heading()).toBeVisible()
    await expect(this.newRunButton()).toBeVisible()
  }
}
