import { fileURLToPath } from 'node:url'
import { expect, test } from '@playwright/test'

const halfMarathon = fileURLToPath(new URL('../../testdata/fit/half_marathon.fit', import.meta.url))

test('upload a run, open it from the list and see its map and charts', async ({ page }) => {
  await page.goto('/register')
  await expect(page.getByRole('heading', { name: 'Create an account' })).toBeVisible()
  await page.getByLabel('Username').fill(`detail-${String(Date.now())}`)
  await page.getByLabel('Password').fill('correct horse battery')
  await page.getByRole('button', { name: 'Register' }).click()
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible()

  await page.goto('/activities')
  await page.getByRole('button', { name: 'Upload' }).click()
  const dialog = page.getByRole('dialog', { name: 'Upload an activity' })
  await dialog.getByLabel('File').setInputFiles(halfMarathon)
  await dialog.getByRole('button', { name: 'Upload' }).click()
  await expect(page.getByRole('status')).toHaveText('Imported 1 activity.')

  await page.getByRole('table').getByRole('link').first().click()
  await expect(page.getByRole('heading', { name: 'Running' })).toBeVisible()
  await expect(page.getByRole('img', { name: 'Heart rate chart' }).locator('canvas')).toBeVisible()
  await expect(page.getByRole('img', { name: 'Pace chart' }).locator('canvas')).toBeVisible()
  await expect(page.getByRole('table', { name: 'Laps' })).toBeVisible()

  // The map needs WebGL2: CI's Chromium has it (SwiftShader); a browser without it
  // gets a note instead, and the rest of the page still works.
  const webgl2 = await page.evaluate<boolean>(
    "document.createElement('canvas').getContext('webgl2') !== null",
  )
  if (!webgl2) {
    await expect(page.getByRole('note')).toHaveText(
      'This browser cannot display maps: it needs WebGL2.',
    )
    return
  }
  await expect(page.getByRole('region', { name: 'Route map' }).locator('canvas')).toBeVisible()
  // Hovering a chart moves the marker on the map.
  await page
    .getByRole('img', { name: 'Heart rate chart' })
    .locator('.u-over')
    .hover({ position: { x: 100, y: 50 } })
  await expect(page.locator('.maplibregl-marker')).toBeVisible()
})
