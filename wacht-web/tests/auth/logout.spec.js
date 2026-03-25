import { expect, test } from '@playwright/test'
import { login } from '../helpers.js'

test('sign out revokes the current session token', async ({ page, request }) => {
  await login(page)

  const token = await page.evaluate(() => localStorage.getItem('wacht_token'))
  expect(token).toBeTruthy()

  await page.getByRole('button', { name: 'Sign out' }).click()

  await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()

  const response = await request.get('/api/auth/me', {
    headers: { Authorization: `Bearer ${token}` },
  })

  expect(response.status()).toBe(401)
  await expect(response.text()).resolves.toBe('unauthorized\n')
})
