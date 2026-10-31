import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { ZoneSet, ZoneSuggestion } from '@/api/zones'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const running: ZoneSet = {
  id: 1,
  sport: 'running',
  effectiveFrom: '2026-09-20',
  lthrBpm: 171,
  thresholdSpeedMS: 1000 / 240,
  zones: {
    hrModel: 'friel_run_hr',
    hr: [
      { name: 'Z1', high: 145.35 },
      { name: 'Z2', low: 145.35, high: 153.9 },
      { name: 'Z5c', low: 182.97 },
    ],
    paceModel: 'friel_run_pace',
    pace: [
      { name: 'Z1', high: 1000 / 312 },
      { name: 'Z5c', low: 1000 / 216 },
    ],
  },
}
const swimming: ZoneSet = {
  id: 2,
  sport: 'swimming',
  effectiveFrom: '2026-01-10',
  cssMS: 1,
  zones: { paceModel: 'css', pace: [{ name: 'Fast', low: 100 / 98 }] },
}

// serve answers the list with items and records what was created and deleted.
function serve(items: ZoneSet[]) {
  const sent: { created?: unknown; csrf?: string | null; deleted?: unknown } = {}
  server.use(
    http.get('/api/zones', () => HttpResponse.json({ items })),
    http.get('/api/zones/suggestions', () => HttpResponse.json({ items: [] })),
    http.post('/api/zones', async ({ request }) => {
      sent.created = await request.json()
      sent.csrf = request.headers.get('X-CSRF-Token')
      return HttpResponse.json(running, { status: 201 })
    }),
    http.delete('/api/zones/:id', ({ params }) => {
      sent.deleted = params.id
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return sent
}

beforeEach(() => {
  loggedInAs(ann)
})

describe('ZonesPage', () => {
  it('lists versions with their zones', async () => {
    serve([running, swimming])
    renderApp('/settings/zones')
    const run = await screen.findByRole('article', { name: 'Running from 20 September 2026' })
    expect(run).toHaveTextContent('LTHR 171 bpm, threshold pace 4:00 /km')
    const hr = within(within(run).getByRole('table', { name: 'Friel, running heart rate' }))
    expect(hr.getAllByRole('row').map((r) => r.textContent)).toEqual([
      'Z1below 145 bpm',
      'Z2145–154 bpm',
      'Z5c183 bpm and above',
    ])
    expect(within(run).getByRole('table', { name: 'Friel, running pace' })).toHaveTextContent(
      'Z1slower than 5:12 /km',
    )
    const swim = screen.getByRole('article', { name: 'Swimming from 10 January 2026' })
    expect(swim).toHaveTextContent('CSS 1:40 /100 m')
    expect(swim).toHaveTextContent('Fast1:38 /100 m or faster')
  })

  it('adds a running version in SI units', async () => {
    const sent = serve([])
    renderApp('/settings/zones')
    expect(await screen.findByText('No zone versions yet.')).toBeInTheDocument()
    const form = within(screen.getByRole('form', { name: 'Add a version' }))
    expect(form.queryByLabelText('FTP (W)')).not.toBeInTheDocument()
    await userEvent.clear(form.getByLabelText('Valid from'))
    await userEvent.type(form.getByLabelText('Valid from'), '2026-09-20')
    await userEvent.type(form.getByLabelText('Lactate threshold HR (bpm)'), '171')
    await userEvent.type(form.getByLabelText('Threshold pace (/km)'), '4:00')
    await userEvent.click(form.getByRole('button', { name: 'Add version' }))

    await screen.findByText('No zone versions yet.')
    expect(sent.csrf).toBe('csrf-ann')
    expect(sent.created).toEqual({
      sport: 'running',
      effectiveFrom: '2026-09-20',
      lthrBpm: 171,
      thresholdSpeedMS: 1000 / 240,
    })
    // The form clears after a save.
    await vi.waitFor(() => {
      const cleared = within(screen.getByRole('form', { name: 'Add a version' }))
      expect(cleared.getByLabelText('Lactate threshold HR (bpm)')).toHaveValue('')
    })
  })

  it('asks each sport for its own thresholds', async () => {
    const sent = serve([])
    renderApp('/settings/zones')
    const form = within(await screen.findByRole('form', { name: 'Add a version' }))
    await userEvent.selectOptions(form.getByLabelText('Sport'), 'Cycling')
    expect(form.getByLabelText('FTP (W)')).toBeInTheDocument()
    expect(form.queryByLabelText('Threshold pace (/km)')).not.toBeInTheDocument()

    await userEvent.selectOptions(form.getByLabelText('Sport'), 'Swimming')
    expect(form.queryByLabelText('Lactate threshold HR (bpm)')).not.toBeInTheDocument()
    await userEvent.type(form.getByLabelText('CSS pace (/100 m)'), '1:40')
    await userEvent.type(form.getByLabelText('Max HR (bpm)'), '190')
    await userEvent.type(form.getByLabelText('Resting HR (bpm)'), '50')
    await userEvent.click(form.getByRole('button', { name: 'Add version' }))
    await vi.waitFor(() => {
      expect(sent.created).toMatchObject({
        sport: 'swimming',
        cssMS: 1,
        maxHrBpm: 190,
        restingHrBpm: 50,
      })
    })
  })

  it('rejects bad input and shows server errors', async () => {
    serve([])
    server.use(
      http.post('/api/zones', () =>
        problem(
          400,
          'invalid-request',
          'Invalid request',
          'running needs lthrBpm or thresholdSpeedMS',
        ),
      ),
    )
    renderApp('/settings/zones')
    const form = within(await screen.findByRole('form', { name: 'Add a version' }))
    await userEvent.type(form.getByLabelText('Threshold pace (/km)'), 'fast')
    await userEvent.click(form.getByRole('button', { name: 'Add version' }))
    expect(form.getByRole('alert')).toHaveTextContent('Enter paces as m:ss.')

    await userEvent.clear(form.getByLabelText('Threshold pace (/km)'))
    await userEvent.type(form.getByLabelText('Max HR (bpm)'), 'lots')
    await userEvent.click(form.getByRole('button', { name: 'Add version' }))
    expect(form.getByRole('alert')).toHaveTextContent('Heart rates and power are whole numbers.')

    await userEvent.clear(form.getByLabelText('Max HR (bpm)'))
    await userEvent.click(form.getByRole('button', { name: 'Add version' }))
    expect(await form.findByText('running needs lthrBpm or thresholdSpeedMS')).toBeInTheDocument()
    // A failed save keeps what was typed.
    expect(form.getByLabelText('Valid from')).not.toHaveValue('')
  })

  it('deletes a version', async () => {
    const sent = serve([running])
    renderApp('/settings/zones')
    const run = await screen.findByRole('article', { name: 'Running from 20 September 2026' })
    await userEvent.click(within(run).getByRole('button', { name: 'Delete' }))
    await vi.waitFor(() => {
      expect(sent.deleted).toBe('1')
    })
  })

  it('shows list errors', async () => {
    server.use(http.get('/api/zones', () => problem(500, 'internal', 'Internal error')))
    renderApp('/settings/zones')
    expect(await screen.findByText('Internal error')).toBeInTheDocument()
  })

  it('is linked from Settings', async () => {
    serve([])
    server.use(http.get('/api/garmin/backfill', () => HttpResponse.json({ started: false })))
    renderApp('/settings')
    await userEvent.click(await screen.findByRole('link', { name: 'Manage zones' }))
    expect(
      await screen.findByRole('heading', { name: 'Training zones', level: 1 }),
    ).toBeInTheDocument()
  })

  it('confirms or dismisses suggestions from recorded files', async () => {
    serve([running])
    const suggestion: ZoneSuggestion = {
      id: 9,
      sport: 'running',
      effectiveFrom: '2026-10-15',
      lthrBpm: 171,
      maxHrBpm: 187,
      restingHrBpm: 58,
      activityId: 4,
      current: { ...running, lthrBpm: 176, maxHrBpm: 187 },
    }
    const answered: string[] = []
    server.use(
      http.get('/api/zones/suggestions', () => HttpResponse.json({ items: [suggestion] })),
      http.post('/api/zones/suggestions/:id/:answer', ({ params, request }) => {
        answered.push(
          `${String(params.answer)} ${String(params.id)} ${request.headers.get('X-CSRF-Token') ?? ''}`,
        )
        return params.answer === 'confirm'
          ? HttpResponse.json(running, { status: 201 })
          : new HttpResponse(null, { status: 204 })
      }),
    )
    renderApp('/settings/zones')
    const card = within(
      await screen.findByRole('article', { name: 'Running from 15 October 2026' }),
    )
    expect(
      card.getByText('LTHR 171 bpm (was 176 bpm), max HR 187 bpm, resting HR 58 bpm'),
    ).toBeInTheDocument()
    expect(card.getByRole('link', { name: 'From this activity' })).toHaveAttribute(
      'href',
      '/activities/4',
    )
    await userEvent.click(card.getByRole('button', { name: 'Confirm' }))
    await userEvent.click(card.getByRole('button', { name: 'Dismiss' }))
    await vi.waitFor(() => {
      expect(answered).toEqual(['confirm 9 csrf-ann', 'dismiss 9 csrf-ann'])
    })
  })

  it('shows thresholds estimated from best efforts', async () => {
    serve([running])
    const estimates: ZoneSuggestion[] = [
      {
        id: 10,
        sport: 'running',
        effectiveFrom: '2026-10-20',
        thresholdSpeedMS: 1000 / 235,
        activityId: 5,
        current: running,
      },
      { id: 11, sport: 'cycling', effectiveFrom: '2026-10-21', ftpW: 128, activityId: 6 },
    ]
    server.use(http.get('/api/zones/suggestions', () => HttpResponse.json({ items: estimates })))
    renderApp('/settings/zones')
    const section = within(await screen.findByRole('region', { name: 'Suggestions' }))
    expect(section.getByRole('heading', { name: 'Suggested thresholds' })).toBeInTheDocument()
    expect(
      within(section.getByRole('article', { name: 'Running from 20 October 2026' })).getByText(
        'threshold pace 3:55 /km (was 4:00 /km)',
      ),
    ).toBeInTheDocument()
    expect(
      within(section.getByRole('article', { name: 'Cycling from 21 October 2026' })).getByText(
        'FTP 128 W',
      ),
    ).toBeInTheDocument()
  })

  it('shows suggestion errors', async () => {
    serve([])
    server.use(
      http.get('/api/zones/suggestions', () =>
        HttpResponse.json({
          items: [{ id: 1, sport: 'general', effectiveFrom: '2026-03-23', maxHrBpm: 192 }],
        }),
      ),
      http.post('/api/zones/suggestions/:id/confirm', () =>
        problem(
          400,
          'invalid-request',
          'Invalid request',
          'general needs maxHrBpm and restingHrBpm',
        ),
      ),
    )
    renderApp('/settings/zones')
    const section = within(await screen.findByRole('region', { name: 'Suggestions' }))
    await userEvent.click(await section.findByRole('button', { name: 'Confirm' }))
    expect(await section.findByText('general needs maxHrBpm and restingHrBpm')).toBeInTheDocument()
  })
})
