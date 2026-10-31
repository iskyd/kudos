import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { Readiness } from '@/api/wellness'
import { toISODay } from '@/lib/format/date'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const today = toISODay(new Date())
const ready: Readiness = {
  day: today,
  status: 'ready',
  baselineDays: 40,
  requiredDays: 14,
  score: 30,
  uncappedScore: 84,
  cap: { entryId: 4, kind: 'illness', max: 30 },
  components: [
    { name: 'hrv', score: 81.6, share: 0.3, value: 62, baseline: 60.4 },
    { name: 'resting_hr', score: 75, share: 0.2, value: 50, baseline: 50 },
    { name: 'sleep', score: 93.75, share: 0.2, value: 27000 },
    { name: 'form', score: 60, share: 0.15, value: -10 },
    { name: 'check_in', score: 62.5, share: 0.15 },
  ],
}

// serve answers the page's lists with nothing and readiness with body,
// recording the days asked for.
function serve(body: () => Response) {
  const days: (string | null)[] = []
  server.use(
    http.get('/api/wellness', () => HttpResponse.json({ items: [] })),
    http.get('/api/health-log', () => HttpResponse.json({ items: [] })),
    http.get('/api/readiness', ({ request }) => {
      days.push(new URL(request.url).searchParams.get('day'))
      return body()
    }),
  )
  return days
}

async function card() {
  return within(await screen.findByRole('region', { name: 'Readiness' }))
}

beforeEach(() => {
  loggedInAs(ann)
})

describe('ReadinessCard', () => {
  it("shows today's score, the cap and the breakdown", async () => {
    const days = serve(() => HttpResponse.json(ready))
    renderApp('/wellness')
    const readiness = await card()
    expect(await readiness.findByText('30')).toBeInTheDocument()
    expect(readiness.getByRole('note')).toHaveTextContent(
      'Capped at 30 by an illness in the health log (84 without it).',
    )
    const rows = within(readiness.getByRole('table', { name: 'Readiness breakdown' })).getAllByRole(
      'row',
    )
    const cells = rows.slice(1).map((r) =>
      within(r)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    )
    expect(cells).toEqual([
      ['HRV', '62 ms', '60 ms', '82', '30%'],
      ['Resting HR', '50 bpm', '50 bpm', '75', '20%'],
      ['Sleep', '7:30:00', '—', '94', '20%'],
      ['Form', '-10 TSB', '—', '60', '15%'],
      ['Check-in', '—', '—', '63', '15%'],
    ])
    expect(days).toEqual([today])
  })

  it('leaves out the cap note without a cap', async () => {
    serve(() => HttpResponse.json({ ...ready, cap: undefined, score: 84 }))
    renderApp('/wellness')
    const readiness = await card()
    expect(await readiness.findByText('84')).toBeInTheDocument()
    expect(readiness.queryByRole('note')).not.toBeInTheDocument()
  })

  it('reports errors', async () => {
    serve(() => problem(500, 'internal', 'Readiness down'))
    renderApp('/wellness')
    expect(await (await card()).findByRole('alert')).toHaveTextContent('Readiness down')
  })

  it('refetches after the health log changes', async () => {
    const days = serve(() => HttpResponse.json(ready))
    server.use(
      http.post('/api/health-log', async ({ request }) =>
        HttpResponse.json({ id: 9, ...((await request.json()) as object) }, { status: 201 }),
      ),
    )
    renderApp('/wellness')
    await (await card()).findByText('30')
    await userEvent.click(screen.getByRole('button', { name: 'Log an entry' }))
    const dialog = screen.getByRole('dialog', { name: 'Log an entry' })
    await userEvent.type(within(dialog).getByLabelText('Description'), 'Knee')
    await userEvent.type(within(dialog).getByLabelText('Start'), '2026-10-01')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await vi.waitFor(() => {
      expect(days).toHaveLength(2)
    })
  })
})
