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
  ['Planned workouts', '/api/planned-workouts'],
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
      await (await card('Planned workouts')).findByText('Nothing planned for the next 7 days.'),
    ).toBeInTheDocument()
    expect(
      await (await card('Coach insight')).findByRole('link', { name: 'Set up the AI coach' }),
    ).toHaveAttribute('href', '/settings/ai-coach')
  })

  it('lists the next 7 days of planned workouts', async () => {
    const asked: (string | null)[][] = []
    server.use(
      http.get('/api/planned-workouts', ({ request }) => {
        const p = new URL(request.url).searchParams
        asked.push([p.get('from'), p.get('to')])
        return HttpResponse.json([
          { id: 3, planId: 1, day: today, sport: 'running', title: 'Easy run', steps: [] },
          { id: 4, planId: 1, day: '2026-11-15', sport: 'running', title: 'Long run', steps: [] },
        ])
      }),
    )
    renderApp('/')
    const c = await card('Planned workouts')
    expect(await c.findByRole('link', { name: 'Easy run' })).toHaveAttribute('href', '/planned/3')
    expect(c.getByText('Today')).toBeInTheDocument()
    expect(c.getByRole('link', { name: 'Long run' })).toHaveAttribute('href', '/planned/4')
    expect(c.getByText('15 November 2026')).toBeInTheDocument()
    expect(asked[0]).toEqual([today, addDays(today, 6)])
  })

  describe('with the AI coach set up', () => {
    beforeEach(() => {
      server.use(
        http.get('/api/llm-provider', () =>
          HttpResponse.json({
            configured: true,
            provider: 'ollama',
            baseUrl: 'http://o.example.test',
            model: 'm',
            keySet: false,
          }),
        ),
      )
    })

    const insight = (day: string, content: string) =>
      http.get('/api/coach-reports', ({ request }) => {
        const query = new URL(request.url).searchParams
        expect(query.get('kind')).toBe('daily_insight')
        expect(query.get('limit')).toBe('1')
        return HttpResponse.json({
          items: [
            {
              id: 1,
              kind: 'daily_insight',
              day,
              promptVersion: 'daily-insight.v1',
              content,
              createdAt: `${day}T06:30:00Z`,
            },
          ],
        })
      })

    it("shows today's insight with its references", async () => {
      server.use(insight(today, 'Easy run [act:7] after [day:2026-11-02].'))
      renderApp('/')
      const c = await card('Coach insight')
      expect(await c.findByRole('link', { name: 'activity 7' })).toHaveAttribute(
        'href',
        '/activities/7',
      )
      expect(c.getByRole('link', { name: '2 November 2026' })).toHaveAttribute(
        'href',
        '/calendar?day=2026-11-02',
      )
      expect(c.getByText('Today')).toBeInTheDocument()
      expect(c.queryByText(/written after the morning sync/)).not.toBeInTheDocument()
    })

    it('shows an older insight until today is written', async () => {
      server.use(insight('2026-11-02', 'Rest well.'))
      renderApp('/')
      const c = await card('Coach insight')
      expect(await c.findByText('Rest well.')).toBeInTheDocument()
      expect(c.getByText('2 November 2026')).toBeInTheDocument()
      expect(c.getByText("Today's insight is written after the morning sync.")).toBeInTheDocument()
    })

    it('shows the insight loading, then failing', async () => {
      server.use(http.get('/api/coach-reports', () => delay('infinite')))
      renderApp('/')
      expect(await (await card('Coach insight')).findByText('Loading…')).toBeInTheDocument()

      server.use(
        http.get('/api/coach-reports', () => problem(500, 'internal', 'Something went wrong')),
      )
      renderApp('/')
      expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong')
    })

    it('says when nothing is written yet', async () => {
      renderApp('/')
      expect(
        await (
          await card('Coach insight')
        ).findByText("Today's insight is written after the morning sync."),
      ).toBeInTheDocument()
    })
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
