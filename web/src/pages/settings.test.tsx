import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { GarminConnection } from '@/api/garmin'
import { toISODay } from '@/lib/format/date'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

function inDays(n: number) {
  const now = new Date()
  return toISODay(new Date(now.getFullYear(), now.getMonth(), now.getDate() + n))
}

const connected: GarminConnection = {
  connected: true,
  status: 'connected',
  keyExpiresOn: inDays(60),
  syncIntervalHours: 1,
}

function garminReturns(connection: GarminConnection) {
  server.use(http.get('/api/garmin', () => HttpResponse.json(connection)))
}

describe('SettingsPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
    garminReturns({ connected: false })
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

  describe('Garmin', () => {
    it('connects with a key and its expiry, then forgets the key', async () => {
      let body: unknown
      let csrf: string | null = null
      server.use(
        http.put('/api/garmin/key', async ({ request }) => {
          body = await request.json()
          csrf = request.headers.get('X-CSRF-Token')
          return HttpResponse.json({ ...connected, keyExpiresOn: '2027-01-28' })
        }),
      )
      renderApp('/settings')
      expect(await screen.findByText(/Not connected/)).toBeInTheDocument()
      await userEvent.type(screen.getByLabelText('Personal API key'), 'secret-key-123')
      await userEvent.type(screen.getByLabelText('Key expires on'), '2027-01-28')
      await userEvent.click(screen.getByRole('button', { name: 'Connect' }))

      expect(await screen.findByText('28 January 2027')).toBeInTheDocument()
      expect(body).toEqual({ key: 'secret-key-123', expiresOn: '2027-01-28' })
      expect(csrf).toBe('csrf-ann')
      expect(screen.getByText('Never')).toBeInTheDocument()
      expect(screen.getByLabelText('Personal API key')).toHaveValue('')
      expect(document.body).not.toHaveTextContent('secret-key-123')
    })

    it('shows a rejected key', async () => {
      server.use(
        http.put('/api/garmin/key', () =>
          problem(422, 'garmin-key-rejected', 'Garmin rejected the key', 'check the key'),
        ),
      )
      renderApp('/settings')
      await userEvent.type(await screen.findByLabelText('Personal API key'), 'bad-key')
      await userEvent.type(screen.getByLabelText('Key expires on'), inDays(30))
      await userEvent.click(screen.getByRole('button', { name: 'Connect' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('check the key')
      expect(screen.getByText(/Not connected/)).toBeInTheDocument()
    })

    it('warns when sync stopped because Garmin rejected the key', async () => {
      garminReturns({ ...connected, status: 'key_rejected' })
      renderApp('/settings')
      expect(await screen.findByRole('alert')).toHaveTextContent(
        'Garmin rejected the key. Sync is paused until you enter a new key.',
      )
    })

    it.each([
      [0, 'The key expires today.'],
      [1, 'The key expires in 1 day.'],
      [7, 'The key expires in 7 days.'],
      [-1, 'The key expired.'],
    ])('warns %i days from expiry', async (days, text) => {
      garminReturns({ ...connected, keyExpiresOn: inDays(days) })
      renderApp('/settings')
      expect(await screen.findByRole('alert')).toHaveTextContent(text)
    })

    it('does not warn more than 7 days from expiry', async () => {
      garminReturns({ ...connected, keyExpiresOn: inDays(8), lastSyncAt: '2026-10-29T07:30:00Z' })
      renderApp('/settings')
      expect(await screen.findByText('Key expires')).toBeInTheDocument()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      expect(screen.queryByText('Never')).not.toBeInTheDocument()
    })

    it('sets the sync interval', async () => {
      garminReturns(connected)
      let body: unknown
      server.use(
        http.put('/api/garmin/sync-interval', async ({ request }) => {
          body = await request.json()
          return HttpResponse.json({ ...connected, syncIntervalHours: 6 })
        }),
      )
      renderApp('/settings')
      await userEvent.selectOptions(await screen.findByLabelText('Sync every'), '6 hours')
      expect(body).toEqual({ hours: 6 })
      expect(await screen.findByLabelText('Sync every')).toHaveValue('6')
    })

    it('shows interval errors', async () => {
      garminReturns(connected)
      server.use(
        http.put('/api/garmin/sync-interval', () =>
          problem(404, 'not-found', 'Not found', 'Garmin is not connected'),
        ),
      )
      renderApp('/settings')
      await userEvent.selectOptions(await screen.findByLabelText('Sync every'), '2 hours')
      expect(await screen.findByRole('alert')).toHaveTextContent('Garmin is not connected')
    })

    it('disconnects', async () => {
      garminReturns(connected)
      server.use(http.delete('/api/garmin', () => new HttpResponse(null, { status: 204 })))
      renderApp('/settings')
      await userEvent.click(await screen.findByRole('button', { name: 'Disconnect' }))
      expect(await screen.findByText(/Not connected/)).toBeInTheDocument()
    })

    it('shows disconnect errors', async () => {
      garminReturns(connected)
      server.use(http.delete('/api/garmin', () => problem(500, 'internal', 'Internal error')))
      renderApp('/settings')
      await userEvent.click(await screen.findByRole('button', { name: 'Disconnect' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('Internal error')
    })

    it('shows loading errors', async () => {
      server.use(http.get('/api/garmin', () => problem(500, 'internal', 'Internal error')))
      renderApp('/settings')
      expect(await screen.findByRole('alert')).toHaveTextContent('Internal error')
    })
  })
})
