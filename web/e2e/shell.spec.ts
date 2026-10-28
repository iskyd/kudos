import { expect, test } from '@playwright/test'

test('loads the app shell and navigates', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveTitle('KudOS')
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible()

  await page.getByRole('link', { name: 'Activities' }).click()
  await expect(page).toHaveURL('/activities')
  await expect(page.getByRole('heading', { name: 'Activities' })).toBeVisible()
})
