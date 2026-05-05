import { expect, test } from '@playwright/test'
import { login, uniqueSuffix } from '../helpers.js'

test('admin can create a probe config from the dashboard', async ({ page }) => {
  await login(page)

  const probeID = `browser-probe-${uniqueSuffix()}`
  const serverURL = await page.evaluate(() => window.location.origin)

  await expect(page.getByRole('heading', { name: 'Probes' })).toBeVisible()
  await page.getByPlaceholder('probe-api-1').fill(probeID)
  await page.getByRole('button', { name: 'Create probe' }).click()

  const config = page.locator('pre').filter({ hasText: `probe_id: ${probeID}` })
  await expect(page.getByText('Copy this reusable probe config now.')).toBeVisible()
  await expect(config).toContainText(`server: ${serverURL}`)
  await expect(config).toContainText(`probe_id: ${probeID}`)
  await expect(config).toContainText(/secret: [a-f0-9]{64}/)
  await expect(config).toContainText('heartbeat_interval: 30s')
  await expect(page.getByRole('button', { name: 'Copy config' })).toBeVisible()
})
