import { expect, test } from '@playwright/test'

// The server talks to e2e/fake-garmin.mjs, which accepts only this key.
const key = 'e2e-personal-api-key'

function inDays(n: number) {
  const d = new Date()
  d.setDate(d.getDate() + n)
  const pad = (v: number) => String(v).padStart(2, '0')
  return `${String(d.getFullYear())}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

test('connect Garmin, import history and sync', async ({ page }) => {
  await page.goto('/register')
  await expect(page.getByRole('heading', { name: 'Create an account' })).toBeVisible()
  await page.getByLabel('Username').fill(`garmin-${String(Date.now())}`)
  await page.getByLabel('Password').fill('correct horse battery')
  await page.getByRole('button', { name: 'Register' }).click()
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible()

  await page.goto('/settings')
  await expect(page.getByText(/Not connected/)).toBeVisible()

  // A key Garmin refuses is not stored.
  await page.getByLabel('Personal API key').fill('wrong-key')
  await page.getByLabel('Key expires on').fill(inDays(90))
  await page.getByRole('button', { name: 'Connect' }).click()
  await expect(page.getByRole('alert')).toContainText('check the key')

  await page.getByLabel('Personal API key').fill(key)
  await page.getByRole('button', { name: 'Connect' }).click()
  await expect(page.getByText('Key expires', { exact: true })).toBeVisible()
  await expect(page.getByLabel('Personal API key')).toHaveValue('')

  // The recorded run imports; progress refreshes until the import is done.
  await page.getByLabel('Days of history').fill('3')
  await page.getByRole('button', { name: 'Import history' }).click()
  await expect(page.getByRole('status').filter({ hasText: /^Imported / })).toBeVisible({
    timeout: 30_000,
  })
  await expect(page.getByText('1 of 1')).toBeVisible()
  await expect(page.getByText('3 of 3')).toBeVisible()

  await page.getByRole('button', { name: 'Sync now' }).click()
  await expect(page.getByText(/^(Sync queued\.|A sync is already running\.)$/)).toBeVisible()
})
