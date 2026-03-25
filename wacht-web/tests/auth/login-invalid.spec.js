import { expect, test } from '@playwright/test'
import { DEV_EMAIL } from '../helpers.js'

test('invalid credentials stay on the sign-in form', async ({ page }) => {
  await page.goto('/')

  await page.locator('input[type="email"]').fill(DEV_EMAIL)
  await page.locator('input[type="password"]').fill('wrong-password')
  await page.getByRole('button', { name: 'Sign in' }).click()

  await expect(page.getByText('invalid credentials')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sign out' })).toHaveCount(0)
})
