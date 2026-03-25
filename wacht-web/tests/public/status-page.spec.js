import { expect, test } from '@playwright/test'
import { login, uniqueSuffix } from '../helpers.js'

test('account page share link opens the anonymous public status page', async ({ page, context }) => {
  await login(page)

  const suffix = uniqueSuffix()
  const checkID = `public-page-${suffix}`
  const target = `https://example.com/public-status-${suffix}`

  await page.getByRole('button', { name: 'Add check' }).click()
  await page.getByPlaceholder('check-my-api').fill(checkID)
  await page.getByPlaceholder('https://example.com').fill(target)
  await page.getByRole('button', { name: 'Add check' }).click()

  await expect(page.getByText(checkID)).toBeVisible()

  await page.getByRole('button', { name: 'Account' }).click()
  await expect(page.getByRole('heading', { name: 'Account' })).toBeVisible()

  const publicURL = await page.getByRole('link', { name: 'Open page' }).getAttribute('href')
  expect(publicURL).toBeTruthy()
  expect(publicURL).toContain('/public/')

  const publicPage = await context.newPage()
  await publicPage.goto(publicURL)

  await expect(publicPage.getByRole('heading', { name: 'Service status' })).toBeVisible()
  await expect(publicPage.getByText(checkID)).toBeVisible()
  await expect(publicPage.getByText(target)).toHaveCount(0)
  await expect(publicPage.getByRole('button', { name: 'Sign out' })).toHaveCount(0)
})
