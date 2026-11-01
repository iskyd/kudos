import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import type { Curves, Volume } from '@/api/analytics'
import { periodStarts, toISODay } from '@/lib/format/date'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const today = toISODay(new Date())
const months = periodStarts('month', today, 12)

const card = async (name: string) => within(await screen.findByRole('region', { name }))

const cards: [string, string][] = [
  ['Volume', '/api/volume'],
  ['Time in zones', '/api/volume'],
  ['Fitness and form', '/api/fitness'],
  ['Curves', '/api/curves'],
]

describe('TrendsPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it.each(cards)('shows %s loading', async (name, path) => {
    server.use(http.get(path, () => delay('infinite')))
    renderApp('/trends')
    expect((await card(name)).getByText('Loading…')).toBeInTheDocument()
  })

  it.each(cards)('shows %s failing', async (name, path) => {
    server.use(http.get(path, () => problem(500, 'internal', 'Something went wrong')))
    renderApp('/trends')
    expect(await (await card(name)).findByRole('alert')).toHaveTextContent('Something went wrong')
  })

  it('shows each card empty without data', async () => {
    renderApp('/trends')
    expect(
      await (await card('Volume')).findByText('No activities in this period.'),
    ).toBeInTheDocument()
    expect(
      await (await card('Time in zones')).findByText('No time in zones in this period.'),
    ).toBeInTheDocument()
    expect(
      await (await card('Curves')).findByText('No power or pace efforts in this period.'),
    ).toBeInTheDocument()
  })

  it('shows volume by period with time in zones, for the chosen period and sport', async () => {
    const asked: Record<string, string | null>[] = []
    const volume: Volume = {
      periods: [
        {
          start: months[0] ?? '',
          activities: 3,
          distanceM: 42000,
          durationS: 12600,
          ascentM: 310.4,
          load: 180.6,
        },
      ],
      timeInZones: [{ metric: 'hr', model: 'friel_run_hr', seconds: [600, 1800, 0] }],
    }
    server.use(
      http.get('/api/volume', ({ request }) => {
        const p = new URL(request.url).searchParams
        asked.push({
          period: p.get('period'),
          from: p.get('from'),
          to: p.get('to'),
          sport: p.get('sport'),
        })
        return HttpResponse.json(volume)
      }),
    )
    renderApp('/trends')

    // Twelve months, this one first, months without activities as zero.
    const rows = within(await screen.findByRole('table', { name: 'Volume' })).getAllByRole('row')
    expect(rows).toHaveLength(13)
    expect(rows[1]).toHaveTextContent(/3\s*3:30:00\s*42\.00 km\s*310 m\s*181/)
    expect(rows[12]).toHaveTextContent(/0\s*0:00\s*0\.00 km\s*0 m\s*0$/)
    expect(asked[0]).toEqual({ period: 'month', from: months.at(-1), to: today, sport: null })

    const zones = await screen.findByRole('table', {
      name: 'Heart rate: Friel, running heart rate',
    })
    expect(within(zones).getAllByRole('row')[1]).toHaveTextContent(/Zone 2\s*30:00\s*75%/)

    await userEvent.selectOptions(screen.getByLabelText('Period'), 'year')
    await userEvent.selectOptions(screen.getByLabelText('Sport (volume and zones)'), 'running')
    expect(await screen.findByText(today.slice(0, 4))).toBeInTheDocument()
    expect(asked.at(-1)).toEqual({
      period: 'year',
      from: periodStarts('year', today, 10).at(-1),
      to: today,
      sport: 'running',
    })
  })

  it('shows the curves with their estimates', async () => {
    const point = (durationS: number, value: number) => ({
      durationS,
      value,
      activityId: 1,
      startTime: '2026-10-20T17:51:37Z',
      utcOffsetS: 7200,
    })
    const curves: Curves = {
      power: [point(5, 600), point(1200, 135)],
      pace: [point(60, 5), point(1800, 3.65)],
      ftpW: 128,
      thresholdSpeedMS: 1000 / 274,
      vo2max: 49.95,
      racePredictions: [{ distanceM: 5000, timeS: 1197 }],
      cssMS: 200 / 190,
    }
    server.use(http.get('/api/curves', () => HttpResponse.json(curves)))
    renderApp('/trends')

    const c = await card('Curves')
    expect(await c.findByRole('img', { name: 'Power curve chart' })).toHaveAttribute(
      'data-points',
      '2',
    )
    expect(c.getByRole('img', { name: 'Pace curve chart' })).toBeInTheDocument()
    const estimates = within(c.getByLabelText('Estimates'))
    for (const text of [
      '128 W',
      '4:34 /km',
      '50.0 ml/kg/min',
      'Predicted 5.00 km',
      '19:57',
      '1:35 /100 m',
    ]) {
      expect(estimates.getByText(text)).toBeInTheDocument()
    }
  })
})
