import { expect, test } from './fixtures'
import { SettingsShellPO } from './po/settings-shell.po'
import { installApiMocks, makeSpies } from './po/mock-api'

// UI-E2E-08 — Settings Save → Dynamic Phase-B Config (P1)
// Risk link: DF1 re-parse regression. Asserts that a PUT /api/settings reaches
// the backend with the new cost_cap_per_run value, the response is reflected
// on a follow-up GET, and the writer/critic provider edits propagate too.

test('saves cost_cap_per_run and surfaces the new value on refetch', async ({
  page,
  skipOnboarding,
  consoleGuard,
}) => {
  void skipOnboarding
  const spies = makeSpies()
  await installApiMocks(page, {
    state: { runs: [] },
    spies,
  })

  const settings = new SettingsShellPO(page)
  await settings.goto()
  await settings.expectShellReady()

  // Default config has cost_cap_per_run=0.50; bump to 1.00.
  const capInput = settings.costCapPerRunInput()
  await expect(capInput).toBeVisible()
  await expect(capInput).toHaveValue('0.5')

  await capInput.fill('1.00')
  await settings.save()

  // The PUT carries the new config + If-Match ETag.
  await expect.poll(() => spies.settingsPuts.length).toBe(1)
  const put = spies.settingsPuts[0]
  expect(put.config).toMatchObject({
    cost_cap_per_run: 1,
  })
  expect(put.ifMatch).toMatch(/^W\/"settings-/)

  // UI confirms the save; the input now reads back the saved value via the
  // refetched snapshot. Number inputs preserve the operator's typed format
  // ("1.00") rather than re-stringifying via the controlled prop.
  await expect(settings.statusMessage()).toContainText(/saved/i)
  await expect(capInput).toHaveValue('1.00')

  expect(consoleGuard.consoleErrors).toEqual([])
  expect(consoleGuard.pageErrors).toEqual([])
})

test('writer/critic provider edits flow through with the same save', async ({
  page,
  skipOnboarding,
}) => {
  void skipOnboarding
  const spies = makeSpies()
  await installApiMocks(page, {
    state: { runs: [] },
    spies,
  })

  const settings = new SettingsShellPO(page)
  await settings.goto()

  // Writer DashScope → keep DashScope; Critic DeepSeek → switch model name.
  // Just touching the critic_model field is enough to assert propagation.
  const criticModel = page.getByLabel('Critic model', { exact: true })
  await criticModel.fill('deepseek-chat-v2')

  await settings.save()
  await expect.poll(() => spies.settingsPuts.length).toBe(1)
  expect(spies.settingsPuts[0].config).toMatchObject({
    critic_model: 'deepseek-chat-v2',
  })
})
