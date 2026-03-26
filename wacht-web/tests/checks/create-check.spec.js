import { expect, test } from '@playwright/test'
import { login, uniqueSuffix } from '../helpers.js'

test('add check creates a new row in the dashboard', async ({ page }) => {
  await login(page)

  const checkID = `browser-check-${uniqueSuffix()}`
  const target = 'https://example.com'

  await page.getByRole('button', { name: 'Add check' }).click()
  await page.getByPlaceholder('check-my-api').fill(checkID)
  await page.getByPlaceholder('https://example.com').fill(target)
  await page.getByRole('button', { name: 'Add check' }).click()

  await expect(page.getByText(checkID)).toBeVisible()
  await expect(page.getByText(target)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Edit' }).first()).toBeVisible()
})
