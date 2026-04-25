import type { Locator, Page } from '@playwright/test'
import { expect } from '@playwright/test'

export class BatchReviewPO {
  readonly page: Page

  constructor(page: Page) {
    this.page = page
  }

  root(): Locator {
    return this.page.getByRole('region', { name: 'Batch review layout' })
  }

  listbox(): Locator {
    return this.page.getByRole('listbox', { name: 'Review scene queue' })
  }

  detailPane(): Locator {
    return this.page.locator('.batch-review__detail-pane')
  }

  sceneCardByIndex(sceneIndex: number): Locator {
    // Scene cards render "Scene {scene_index + 1}" in the eyebrow.
    return this.page
      .getByRole('option')
      .filter({ hasText: `Scene ${sceneIndex + 1}` })
      .first()
  }

  selectedSceneCard(): Locator {
    return this.page.locator('.scene-card[data-selected="true"]').first()
  }

  approveButton(): Locator {
    return this.page.getByRole('button', { name: '[Enter] Approve' })
  }

  rejectButton(): Locator {
    return this.page.getByRole('button', { name: '[Esc] Reject' })
  }

  approveAllButton(): Locator {
    return this.page.getByRole('button', {
      name: '[Shift+Enter] Approve All Remaining',
    })
  }

  skipButton(): Locator {
    return this.page.getByRole('button', { name: '[S] Skip' })
  }

  undoButton(): Locator {
    return this.page.getByRole('button', { name: '[Ctrl+Z] Undo' })
  }

  rejectComposer(): Locator {
    // RejectComposer renders a textarea for the reason note.
    return this.page
      .getByRole('region')
      .filter({ hasText: /reject/i })
      .first()
  }

  approveAllConfirmPanel(): Locator {
    // InlineConfirmPanel shows "This will approve {n}".
    return this.page.getByText(/This will approve/i).first()
  }

  async focusRoot() {
    // The <section> has tabIndex=-1; clicking the listbox parent focuses root
    // via the scroll helper without selecting a card.
    await this.root().click({ position: { x: 1, y: 1 } })
  }

  async pressChord(key: string) {
    await this.page.keyboard.press(key)
  }

  async expectSelected(sceneIndex: number) {
    await expect(
      this.page.locator(
        `.scene-card[data-selected="true"]:has-text("Scene ${sceneIndex + 1}")`,
      ),
    ).toBeVisible()
  }
}
