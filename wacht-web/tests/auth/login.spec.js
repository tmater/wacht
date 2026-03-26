import { expect, test } from '@playwright/test'
import { DEV_EMAIL, login } from '../helpers.js'

test('sign in loads the dashboard', async ({ page }) => {
  await login(page)

  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add check' })).toBeVisible()
  await expect(page.getByText(DEV_EMAIL)).toBeVisible()

  const token = await page.evaluate(() => localStorage.getItem('wacht_token'))
  expect(token).toBeTruthy()
})
