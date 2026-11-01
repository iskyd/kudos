import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import type { ActivitySummary } from '@/api/activities'
import type { Calendar } from '@/api/analytics'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const run = (id: number, startTime: string, utcOffsetS: number): ActivitySummary => ({
  id,
  sport: 'running',
  subtype: 'outdoor',
  source: 'upload',
  startTime,
  utcOffsetS,
  timerTimeS: 1800,
  distanceM: 6000,
})

const day = async (name: string) => within(await screen.findByRole('listitem', { name }))

describe('CalendarPage', () => {
  beforeEach(() => {
    // Only the clock: today is 25 October 2026, the day summer time ends.
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 9, 25, 9))
    loggedInAs(ann)
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows loading, then a failure', async () => {
    server.use(http.get('/api/calendar', () => delay('infinite')))
    renderApp('/calendar')
    expect(await screen.findByText('Loading…')).toBeInTheDocument()

    server.use(http.get('/api/calendar', () => problem(500, 'internal', 'Something went wrong')))
    renderApp('/calendar')
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong')
  })

  it('shows an empty month', async () => {
    renderApp('/calendar')
    expect(await screen.findByText('Nothing recorded in this month.')).toBeInTheDocument()
    const month = screen.getByRole('list', { name: 'October 2026' })
    expect(within(month).getAllByRole('listitem')).toHaveLength(35)
    expect(screen.getByRole('listitem', { name: '25 October 2026' })).toHaveAttribute(
      'aria-current',
      'date',
    )
  })

  it('places activities on the local day where they happened', async () => {
    const asked: (string | null)[][] = []
    const calendar: Calendar = {
      activities: [
        // 00:30 at +02:00 on the 25th, still the 24th in UTC.
        run(1, '2026-10-24T22:30:00Z', 7200),
        // 23:30 at +01:00 on the 25th, after the change.
        run(2, '2026-10-25T22:30:00Z', 3600),
        // 00:30 at +01:00 on the 26th.
        run(3, '2026-10-25T23:30:00Z', 3600),
      ],
      wellness: [
        { day: '2026-10-20', soreness: 2 },
        // Body metrics alone are no check-in.
        { day: '2026-10-21', weightKg: 70 },
      ],
      healthLog: [
        {
          id: 1,
          kind: 'injury',
          description: 'Sore knee',
          startDate: '2026-10-01',
          endDate: '2026-10-03',
        },
        { id: 2, kind: 'illness', description: 'Cold', startDate: '2026-10-30' },
      ],
    }
    server.use(
      http.get('/api/calendar', ({ request }) => {
        const p = new URL(request.url).searchParams
        asked.push([p.get('from'), p.get('to')])
        return HttpResponse.json(calendar)
      }),
    )
    renderApp('/calendar')

    const sunday = await day('25 October 2026')
    const links = await sunday.findAllByRole('link')
    expect(links.map((l) => l.getAttribute('href'))).toEqual(['/activities/1', '/activities/2'])
    expect(links[0]).toHaveTextContent('Running00:30 · 30:00 · 6.00 km')
    expect(links[1]).toHaveTextContent('23:30')
    expect((await day('24 October 2026')).queryByRole('link')).not.toBeInTheDocument()
    expect((await day('26 October 2026')).getByRole('link')).toHaveAttribute(
      'href',
      '/activities/3',
    )
    expect(asked[0]).toEqual(['2026-09-28', '2026-11-01'])

    expect((await day('20 October 2026')).getByText('Check-in')).toBeInTheDocument()
    expect((await day('21 October 2026')).queryByText('Check-in')).not.toBeInTheDocument()
    expect((await day('3 October 2026')).getByText('Injury: Sore knee')).toBeInTheDocument()
    expect((await day('4 October 2026')).queryByText(/Injury/)).not.toBeInTheDocument()
    // Ongoing: every day from its start.
    expect((await day('1 November 2026')).getByText('Illness: Cold')).toBeInTheDocument()
  })

  it('moves by month and week', async () => {
    const asked: (string | null)[][] = []
    server.use(
      http.get('/api/calendar', ({ request }) => {
        const p = new URL(request.url).searchParams
        asked.push([p.get('from'), p.get('to')])
        return HttpResponse.json({ activities: [], wellness: [], healthLog: [] })
      }),
    )
    renderApp('/calendar')
    expect(await screen.findByRole('heading', { name: 'October 2026' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(await screen.findByRole('heading', { name: 'November 2026' })).toBeInTheDocument()
    expect(asked.at(-1)).toEqual(['2026-10-26', '2026-12-06'])

    await userEvent.click(screen.getByRole('button', { name: 'Today' }))
    await userEvent.selectOptions(screen.getByLabelText('View'), 'week')
    expect(
      await screen.findByRole('heading', { name: 'Week of 19 October 2026' }),
    ).toBeInTheDocument()
    expect(asked.at(-1)).toEqual(['2026-10-19', '2026-10-25'])
    expect(screen.getAllByRole('listitem')).toHaveLength(7)

    await userEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(
      await screen.findByRole('heading', { name: 'Week of 26 October 2026' }),
    ).toBeInTheDocument()
    expect(asked.at(-1)).toEqual(['2026-10-26', '2026-11-01'])
    expect(await screen.findByText('Nothing recorded in this week.')).toBeInTheDocument()
  })
})
