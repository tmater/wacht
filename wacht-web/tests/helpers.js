/* global process */
import { expect } from '@playwright/test'

export const DEV_EMAIL = process.env.E2E_EMAIL ?? 'browser@wacht.local'
export const DEV_PASSWORD = process.env.E2E_PASSWORD ?? 'browser-password-a13f6d8c'

export async function login(page, { email = DEV_EMAIL, password = DEV_PASSWORD } = {}) {
  await page.goto('/')
  await page.locator('input[type="email"]').fill(email)
  await page.locator('input[type="password"]').fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
}

export function uniqueSuffix() {
  return `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}
