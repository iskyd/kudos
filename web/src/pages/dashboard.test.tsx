import { screen, within } from '@testing-library/react'
import { delay, http, HttpResponse } from 'msw'
import type { ActivitySummary } from '@/api/activities'
import type { FitnessDay, Volume } from '@/api/analytics'
import { addDays, mondayOf, toISODay } from '@/lib/format/date'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const today = toISODay(new Date())
const monday = mondayOf(today)

const card = async (name: string) => within(await screen.findByRole('region', { name }))

// Each card's endpoint, for the loading and error states.
const endpoints: [string, string][] = [
  ['Fitness and form', '/api/fitness'],
  ['Weekly volume', '/api/volume'],
  ['Latest activities', '/api/activities'],
  ['Sync', '/api/garmin'],
  ['Readiness', '/api/readiness'],
]

describe('DashboardPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it.each(endpoints)('shows %s loading', async (name, path) => {
    server.use(http.get(path, () => delay('infinite')))
    renderApp('/')
    expect((await card(name)).getByText('Loading…')).toBeInTheDocument()
  })

  it.each(endpoints)('shows %s failing', async (name, path) => {
    server.use(http.get(path, () => problem(500, 'internal', 'Something went wrong')))
    renderApp('/')
    expect(await (await card(name)).findByRole('alert')).toHaveTextContent('Something went wrong')
  })

  it('shows each card empty without data', async () => {
    renderApp('/')
    expect(
      await (await card('Fitness and form')).findByText('No training load in this period.'),
    ).toBeInTheDocument()
    expect(
      await (await card('Weekly volume')).findByText('No activities in the last 12 weeks.'),
    ).toBeInTheDocument()
    expect(
      await (await card('Latest activities')).findByText(/No activities yet/),
    ).toBeInTheDocument()
    expect(await (await card('Sync')).findByText(/Garmin is not connected/)).toBeInTheDocument()
    expect(
      (await card('Planned workouts')).getByText(/once training plans are available/),
    ).toBeInTheDocument()
    expect(
      (await card('Coach insight')).getByText(/once the AI coach is available/),
    ).toBeInTheDocument()
  })

  it('shows fitness, volume, latest activities and sync', async () => {
    const days: FitnessDay[] = [
      { day: addDays(today, -1), load: 0, ctl: 40.2, atl: 55.4, tsb: -12.6 },
      { day: today, load: 80, ctl: 41.1, atl: 58.7, tsb: -15.2 },
    ]
    const volume: Volume = {
      periods: [
        {
          start: addDays(monday, -14),
          activities: 2,
          distanceM: 15000,
          durationS: 5400,
          ascentM: 0,
          load: 90.4,
        },
        { start: monday, activities: 1, distanceM: 10000, durationS: 3000, ascentM: 50, load: 50 },
      ],
      timeInZones: [],
    }
    const run = {
      id: 7,
      sport: 'running',
      subtype: 'outdoor',
      startTime: '2026-10-25T06:30:00Z',
      utcOffsetS: 3600,
      timerTimeS: 3000,
      distanceM: 10000,
    } as ActivitySummary
    const asked: (string | null)[] = []
    server.use(
      http.get('/api/fitness', () => HttpResponse.json(days)),
      http.get('/api/volume', ({ request }) => {
        const url = new URL(request.url)
        asked.push(
          url.searchParams.get('period'),
          url.searchParams.get('from'),
          url.searchParams.get('to'),
        )
        return HttpResponse.json(volume)
      }),
      http.get('/api/activities', () => HttpResponse.json({ items: [run] })),
      http.get('/api/garmin', () =>
        HttpResponse.json({
          connected: true,
          status: 'connected',
          lastSyncAt: '2026-10-31T06:00:00',
          syncedThrough: '2026-10-30',
          syncIntervalHours: 6,
          lastError: 'Garmin was unavailable',
        }),
      ),
    )
    renderApp('/')

    const fitness = await card('Fitness and form')
    expect(await fitness.findByText('Fitness')).toBeInTheDocument()
    expect(fitness.getByText('41')).toBeInTheDocument()
    expect(fitness.getByText('59')).toBeInTheDocument()
    expect(fitness.getByText('-15')).toBeInTheDocument()

    // Twelve weeks, this one first, the weeks without activities as zero.
    const rows = within(await screen.findByRole('table', { name: 'Weekly volume' })).getAllByRole(
      'row',
    )
    expect(rows).toHaveLength(13)
    expect(rows[1]).toHaveTextContent(/This week\s*1\s*50:00\s*10\.00 km\s*50/)
    expect(rows[2]).toHaveTextContent(/0\s*0:00\s*0\.00 km\s*0$/)
    expect(rows[3]).toHaveTextContent(/2\s*1:30:00\s*15\.00 km\s*90/)
    expect(asked).toEqual(['week', addDays(monday, -77), today])

    const latest = await card('Latest activities')
    expect(await latest.findByRole('link', { name: /25 Oct/ })).toHaveAttribute(
      'href',
      '/activities/7',
    )

    const sync = await card('Sync')
    expect(await sync.findByText('31 October 2026 at 06:00')).toBeInTheDocument()
    expect(sync.getByText('30 October 2026')).toBeInTheDocument()
    expect(sync.getByText('6 h')).toBeInTheDocument()
    expect(sync.getByText('Garmin was unavailable')).toBeInTheDocument()
  })
})
