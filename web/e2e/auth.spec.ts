import { expect, test } from '@playwright/test'

test('register, navigate, log out and log back in', async ({ page }) => {
  const username = `e2e-${String(Date.now())}`
  const password = 'correct horse battery'

  await page.goto('/')
  await expect(page).toHaveTitle('KudOS')
  await expect(page).toHaveURL('/login')

  await page.getByRole('link', { name: 'Register' }).click()
  // Wait for the register form: typing earlier fills the login form being replaced.
  await expect(page.getByRole('heading', { name: 'Create an account' })).toBeVisible()
  await page.getByLabel('Username').fill(username)
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Register' }).click()
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible()

  await page.getByRole('link', { name: 'Activities' }).click()
  await expect(page).toHaveURL('/activities')
  await expect(page.getByRole('heading', { name: 'Activities' })).toBeVisible()

  await page.getByRole('button', { name: 'Log out' }).click()
  await expect(page).toHaveURL('/login')
  await page.goto('/settings')
  await expect(page).toHaveURL('/login')
  await expect(page.getByRole('heading', { name: 'Log in' })).toBeVisible()

  await page.getByLabel('Username').fill(username)
  await page.getByLabel('Password').fill('wrong password')
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page.getByRole('alert')).toHaveText('Invalid username or password')

  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).toHaveURL('/settings')
  await expect(page.getByText(`Logged in as ${username}`)).toBeVisible()

  await page.getByRole('button', { name: 'Log out' }).click()
  await expect(page).toHaveURL('/login')
})
