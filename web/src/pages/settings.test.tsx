import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

describe('SettingsPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it('shows the profile', async () => {
    renderApp('/settings')
    expect(await screen.findByRole('heading', { name: 'Profile' })).toBeInTheDocument()
    expect(screen.getByText(/Logged in as/)).toHaveTextContent('Logged in as ann')
    expect(screen.getByLabelText('Timezone')).toHaveValue('Europe/Rome')
  })

  it('lists a stored timezone the browser does not know', async () => {
    loggedInAs({ ...ann, timezone: 'Etc/Unknown' })
    renderApp('/settings')
    expect(await screen.findByLabelText('Timezone')).toHaveValue('Etc/Unknown')
  })

  it('saves the timezone with the CSRF token', async () => {
    let body: unknown
    let csrf: string | null = null
    server.use(
      http.patch('/api/me', async ({ request }) => {
        body = await request.json()
        csrf = request.headers.get('X-CSRF-Token')
        return HttpResponse.json({ ...ann, timezone: 'America/New_York' })
      }),
    )
    renderApp('/settings')
    await userEvent.selectOptions(await screen.findByLabelText('Timezone'), 'America/New_York')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Saved.')
    expect(body).toEqual({ timezone: 'America/New_York' })
    expect(csrf).toBe('csrf-ann')
    expect(screen.getByLabelText('Timezone')).toHaveValue('America/New_York')
  })

  it('shows save errors', async () => {
    server.use(
      http.patch('/api/me', () =>
        problem(400, 'invalid-request', 'Invalid request', 'timezone must be an IANA name'),
      ),
    )
    renderApp('/settings')
    await userEvent.click(await screen.findByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('timezone must be an IANA name')
  })
})
