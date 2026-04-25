import type { Locator, Page } from '@playwright/test'
import { expect } from '@playwright/test'
import { dismissOnboardingIfVisible } from '../fixtures'

export class SettingsShellPO {
  readonly page: Page

  constructor(page: Page) {
    this.page = page
  }

  async goto() {
    await this.page.goto('/settings')
    await dismissOnboardingIfVisible(this.page)
  }

  heading(): Locator {
    return this.page.getByRole('heading', { name: 'Settings', level: 1 })
  }

  modelsCard(): Locator {
    return this.page
      .getByRole('region')
      .filter({ hasText: 'Models and cost guardrails' })
      .first()
  }

  costCapPerRunInput(): Locator {
    // <label><span>Run hard cap</span><input type="number" …/></label>
    return this.page.getByLabel('Run hard cap', { exact: true })
  }

  writerProviderInput(): Locator {
    return this.page.getByLabel('Writer provider', { exact: true })
  }

  criticProviderInput(): Locator {
    return this.page.getByLabel('Critic provider', { exact: true })
  }

  saveButton(): Locator {
    return this.page.getByRole('button', { name: 'Save settings' })
  }

  statusMessage(): Locator {
    return this.page.locator('.settings-workspace__status')
  }

  async fillCostCapPerRun(value: string) {
    const input = this.costCapPerRunInput()
    await input.fill(value)
  }

  async save() {
    await this.saveButton().click()
  }

  async expectShellReady() {
    await expect(this.heading()).toBeVisible()
    await expect(this.modelsCard()).toBeVisible()
    await expect(this.saveButton()).toBeVisible()
  }
}
