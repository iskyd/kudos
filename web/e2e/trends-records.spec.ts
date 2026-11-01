import { fileURLToPath } from 'node:url'
import { expect, test } from '@playwright/test'

const corpus = (name: string) =>
  fileURLToPath(new URL(`../../testdata/fit/${name}`, import.meta.url))

test('records and trends render from imported activities', async ({ page }) => {
  await page.goto('/register')
  await page.getByLabel('Username').fill(`trends-${String(Date.now())}`)
  await page.getByLabel('Password').fill('correct horse battery')
  await page.getByRole('button', { name: 'Register' }).click()
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible()

  // A run (pace curve, running records) and a Zwift ride (power curve and records).
  await page.goto('/activities')
  for (const file of ['half_marathon.fit', 'zwift.fit']) {
    await page.getByRole('button', { name: 'Upload' }).click()
    const dialog = page.getByRole('dialog', { name: 'Upload an activity' })
    await dialog.getByLabel('File').setInputFiles(corpus(file))
    await dialog.getByRole('button', { name: 'Upload' }).click()
    await expect(page.getByRole('status')).toHaveText('Imported 1 activity.')
  }

  // Records come from the derived values a background job computes after import.
  await page.goto('/records')
  await expect(async () => {
    await page.reload()
    await expect(page.getByRole('cell', { name: 'Fastest 5K' })).toBeVisible({ timeout: 1000 })
  }).toPass({ timeout: 30_000 })
  await expect(page.getByRole('cell', { name: 'Best 20 min power' })).toBeVisible()

  await page.goto('/trends')
  await page.getByLabel('Period').selectOption('year')
  await expect(
    page.getByRole('table', { name: 'Volume' }).getByRole('row', { name: /^2026/ }),
  ).toContainText('2')
  await expect(page.getByRole('img', { name: 'Power curve chart' }).locator('canvas')).toBeVisible()
  await expect(page.getByRole('img', { name: 'Pace curve chart' }).locator('canvas')).toBeVisible()
  await expect(page.getByLabel('Estimates')).toContainText('Estimated FTP')
})
