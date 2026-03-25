import { expect, test } from '@playwright/test'
import { uniqueSuffix } from '../helpers.js'

test('request access submits from the login page', async ({ page }) => {
  await page.goto('/')

  await page.getByRole('button', { name: 'Request access' }).click()
  await page.locator('input[type="email"]').fill(`browser-request-${uniqueSuffix()}@wacht.local`)
  await page.getByRole('button', { name: 'Request access' }).click()

  await expect(page.getByText('Request received. You will be contacted when your account is approved.')).toBeVisible()
  await expect(page.getByRole('button', { name: '← Back to sign in' })).toBeVisible()
})
