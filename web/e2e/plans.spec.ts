import { expect, test } from '@playwright/test'

// The AI coach is e2e/fake-garmin.mjs, which plans two workouts on the first two days.
const fakeLLM = 'http://127.0.0.1:4174/llm/v1'

test('generate a plan, approve one change and reject the other', async ({ page }) => {
  await page.goto('/register')
  await page.getByLabel('Username').fill(`plans-${String(Date.now())}`)
  await page.getByLabel('Password').fill('correct horse battery')
  await page.getByRole('button', { name: 'Register' }).click()
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible()

  await page.goto('/settings/ai-coach')
  await page.getByLabel('Base URL (optional)').fill(fakeLLM)
  await page.getByLabel('Model').fill('fake')
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByText('Saved.')).toBeVisible()

  await page.goto('/plans')
  await expect(page.getByText(/No plan yet/)).toBeVisible()
  await page.getByRole('button', { name: 'Plan the next 14 days' }).click()
  // The job fills the proposal in; the page polls until it does.
  const items = page.getByRole('listitem').filter({ hasText: /^Added · / })
  await expect(items).toHaveCount(2, { timeout: 30_000 })
  const easy = items.filter({ hasText: 'E2E easy run' })
  const long = items.filter({ hasText: 'E2E long run' })

  // Nothing is planned before approval.
  await page.goto('/')
  await expect(page.getByText('Nothing planned for the next 7 days.')).toBeVisible()

  await page.goto('/plans')
  await easy.getByRole('button', { name: 'Approve' }).click()
  await expect(easy.getByText('Approved')).toBeVisible()
  await long.getByRole('button', { name: 'Reject' }).click()
  await expect(long.getByText('Rejected')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Approve all' })).toBeHidden()

  // Only the approved workout is planned.
  await page.goto('/')
  await expect(page.getByRole('link', { name: /E2E easy run/ })).toBeVisible()
  await expect(page.getByText('E2E long run')).toBeHidden()
})
