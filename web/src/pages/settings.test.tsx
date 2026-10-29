import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { GarminBackfill, GarminConnection } from '@/api/garmin'
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

function backfillReturns(backfill: GarminBackfill) {
  server.use(http.get('/api/garmin/backfill', () => HttpResponse.json(backfill)))
}

const running: GarminBackfill = {
  started: true,
  status: 'running',
  windowStart: '2026-07-02',
  windowEnd: '2026-10-29',
  activitiesTotal: 40,
  activitiesDone: 10,
  activitiesFailed: 0,
  daysTotal: 120,
  daysDone: 0,
}

describe('SettingsPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
    garminReturns({ connected: false })
    backfillReturns({ started: false })
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

  describe('history import', () => {
    beforeEach(() => {
      garminReturns(connected)
    })

    it('starts with the chosen number of days', async () => {
      let body: unknown
      server.use(
        http.post('/api/garmin/backfill', async ({ request }) => {
          body = await request.json()
          return HttpResponse.json(running, { status: 202 })
        }),
      )
      renderApp('/settings')
      const days = await screen.findByLabelText('Days of history')
      expect(days).toHaveValue(120)
      await userEvent.clear(days)
      await userEvent.type(days, '200')
      await userEvent.click(screen.getByRole('button', { name: 'Import history' }))
      expect(await screen.findByRole('status')).toHaveTextContent(
        'Importing 2 July 2026 to 29 October 2026…',
      )
      expect(body).toEqual({ days: 200 })
    })

    it('shows start errors', async () => {
      server.use(
        http.post('/api/garmin/backfill', () =>
          problem(
            409,
            'backfill-exists',
            'Backfill already started',
            'resume the existing backfill instead',
          ),
        ),
      )
      renderApp('/settings')
      await userEvent.click(await screen.findByRole('button', { name: 'Import history' }))
      expect(await screen.findByRole('alert')).toHaveTextContent(
        'resume the existing backfill instead',
      )
    })

    it('shows progress', async () => {
      backfillReturns({ ...running, activitiesDone: 9, activitiesFailed: 1, daysDone: 30 })
      server.use(http.get('/api/garmin/backfill/failures', () => HttpResponse.json({ items: [] })))
      renderApp('/settings')
      const activities = await screen.findByLabelText('Activities')
      expect(activities).toHaveAttribute('value', '10')
      expect(activities).toHaveAttribute('max', '40')
      expect(screen.getByText('10 of 40')).toBeInTheDocument()
      expect(screen.getByLabelText('Health days')).toHaveAttribute('value', '30')
      expect(screen.getByText('30 of 120')).toBeInTheDocument()
    })

    it.each([
      ['rate_limited', 'Paused: Garmin is limiting requests. It continues by itself.', false],
      ['key_rejected', 'Paused until you enter a new key.', true],
      ['disconnected', 'Paused: Garmin is disconnected.', true],
    ] as const)('explains a %s pause', async (reason, text, resumable) => {
      backfillReturns({ ...running, status: 'paused', pauseReason: reason })
      renderApp('/settings')
      expect(await screen.findByText(text)).toBeInTheDocument()
      expect(Boolean(screen.queryByRole('button', { name: 'Resume' }))).toBe(resumable)
    })

    it('resumes a paused backfill', async () => {
      backfillReturns({ ...running, status: 'paused', pauseReason: 'disconnected' })
      let body: unknown
      server.use(
        http.post('/api/garmin/backfill/resume', async ({ request }) => {
          body = await request.json()
          return HttpResponse.json(running)
        }),
      )
      renderApp('/settings')
      await userEvent.click(await screen.findByRole('button', { name: 'Resume' }))
      expect(
        await screen.findByText('Importing 2 July 2026 to 29 October 2026…'),
      ).toBeInTheDocument()
      expect(body).toEqual({ retryFailed: false })
    })

    it('lists failures page by page and retries them', async () => {
      backfillReturns({ ...running, status: 'done', activitiesDone: 37, activitiesFailed: 3 })
      server.use(
        http.get('/api/garmin/backfill/failures', ({ request }) => {
          const cursor = new URL(request.url).searchParams.get('cursor')
          return cursor === 'page-2'
            ? HttpResponse.json({
                items: [
                  {
                    sourceActivityId: '3',
                    startTime: '2026-08-03T06:00:00Z',
                    reason: 'download failed: status 404',
                  },
                ],
              })
            : HttpResponse.json({
                items: [
                  {
                    sourceActivityId: '1',
                    startTime: '2026-08-01T06:00:00Z',
                    reason: 'invalid file: truncated',
                  },
                  {
                    sourceActivityId: '2',
                    startTime: '2026-08-02T06:00:00Z',
                    reason: 'invalid file: truncated',
                  },
                ],
                nextCursor: 'page-2',
              })
        }),
      )
      let body: unknown
      server.use(
        http.post('/api/garmin/backfill/resume', async ({ request }) => {
          body = await request.json()
          return HttpResponse.json({ ...running, activitiesDone: 37 })
        }),
      )
      renderApp('/settings')
      expect(
        await screen.findByText('Imported 2 July 2026 to 29 October 2026.'),
      ).toBeInTheDocument()
      expect(screen.getByText('3 activities could not be imported')).toBeInTheDocument()
      expect(await screen.findAllByText(/invalid file: truncated/)).toHaveLength(2)
      await userEvent.click(screen.getByRole('button', { name: 'Show more' }))
      expect(await screen.findByText(/download failed: status 404/)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Retry failed' }))
      expect(
        await screen.findByText('Importing 2 July 2026 to 29 October 2026…'),
      ).toBeInTheDocument()
      expect(body).toEqual({ retryFailed: true })
    })

    it('shows failure list errors', async () => {
      backfillReturns({ ...running, activitiesFailed: 1 })
      server.use(
        http.get('/api/garmin/backfill/failures', () => problem(500, 'internal', 'Internal error')),
      )
      renderApp('/settings')
      expect(await screen.findByText('1 activity could not be imported')).toBeInTheDocument()
      expect(await screen.findByRole('alert')).toHaveTextContent('Internal error')
    })

    it('shows resume errors', async () => {
      backfillReturns({ ...running, status: 'paused', pauseReason: 'key_rejected' })
      server.use(
        http.post('/api/garmin/backfill/resume', () =>
          problem(404, 'not-found', 'Not found', 'no backfill was started'),
        ),
      )
      renderApp('/settings')
      await userEvent.click(await screen.findByRole('button', { name: 'Resume' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('no backfill was started')
    })

    it('shows progress errors', async () => {
      server.use(http.get('/api/garmin/backfill', () => problem(500, 'internal', 'Internal error')))
      renderApp('/settings')
      expect(await screen.findByRole('alert')).toHaveTextContent('Internal error')
    })
  })
})
