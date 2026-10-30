import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { ActivityComparison, ActivityDetail } from '@/api/activities'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const run: ActivityDetail = {
  id: 1,
  sport: 'running',
  subtype: 'outdoor',
  source: 'upload',
  startTime: '2026-10-10T05:30:00Z',
  utcOffsetS: 7200,
  timerTimeS: 3000,
  distanceM: 10000,
  avgSpeedMS: 1000 / 300,
  avgHrBpm: 150,
  painTags: [],
  legs: [],
  edits: [],
}
const other: ActivityDetail = {
  ...run,
  id: 2,
  startTime: '2026-10-03T05:30:00Z',
  timerTimeS: 2800,
  avgSpeedMS: 1000 / 280,
  avgHrBpm: 158,
}

// a recorded heart rate and speed, b heart rate and power.
const aligned: ActivityComparison = {
  axis: 'distance',
  x: [0, 5000, 10000],
  a: { hrBpm: [140, 150, null], speedMS: [3, 3.5, null] },
  b: { hrBpm: [145, 155, 160], powerW: [250, 260, 270] },
}

// serve answers with a and b and records the axes the comparison was asked for.
function serve(a: ActivityDetail, b: ActivityDetail) {
  const asked: (string | null)[] = []
  server.use(
    http.get('/api/activities/:id', ({ params }) => HttpResponse.json(params.id === '1' ? a : b)),
    http.get('/api/activities/:id/comparison', ({ request }) => {
      const q = new URL(request.url).searchParams
      asked.push(`${q.get('axis') ?? ''} with ${q.get('with') ?? ''}`)
      return HttpResponse.json(aligned)
    }),
  )
  return asked
}

beforeEach(() => {
  loggedInAs(ann)
})

describe('ActivityComparePage', () => {
  it('shows both summaries and overlays the charts by distance', async () => {
    const asked = serve(run, other)
    renderApp('/activities/1/compare/2')
    const rows = within(await screen.findByRole('table')).getAllByRole('row')
    expect(rows[0]).toHaveTextContent('10 Oct 2026, 07:30 (solid)3 Oct 2026, 07:30 (dashed)')
    expect(rows[3]).toHaveTextContent('Duration50:0046:40')
    expect(rows[4]).toHaveTextContent('Avg pace5:00 /km4:40 /km')
    expect(rows[5]).toHaveTextContent('Avg heart rate150 bpm158 bpm')

    const hr = await screen.findByRole('img', { name: 'Heart rate chart' })
    expect(hr).toHaveAttribute('data-series', '2')
    expect(hr).toHaveAttribute('data-points', '3')
    // Each activity's own channels are charted, with the other side empty.
    expect(screen.getAllByRole('img').map((el) => el.getAttribute('aria-label'))).toEqual([
      'Heart rate chart',
      'Pace chart',
      'Power chart',
    ])
    expect(screen.getByRole('button', { name: 'By distance' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )

    await userEvent.click(screen.getByRole('button', { name: 'By time' }))
    expect(screen.getByRole('button', { name: 'By time' })).toHaveAttribute('aria-pressed', 'true')
    await screen.findByRole('img', { name: 'Heart rate chart' })
    expect(asked).toEqual(['distance with 2', 'time with 2'])
  })

  it('aligns by time only when one has no distance', async () => {
    const asked = serve(run, { ...other, distanceM: undefined })
    renderApp('/activities/1/compare/2')
    await screen.findByRole('img', { name: 'Heart rate chart' })
    expect(screen.getByRole('button', { name: 'By distance' })).toBeDisabled()
    expect(asked).toEqual(['time with 2'])
  })

  it('says when neither has samples', async () => {
    serve(run, other)
    server.use(
      http.get('/api/activities/:id/comparison', () =>
        HttpResponse.json({ axis: 'time', x: [0, 0], a: {}, b: {} }),
      ),
    )
    renderApp('/activities/1/compare/2')
    expect(await screen.findByText('Neither activity has samples to chart.')).toBeInTheDocument()
  })

  it('shows comparison errors', async () => {
    serve(run, other)
    server.use(
      http.get('/api/activities/:id/comparison', () =>
        problem(400, 'invalid-request', 'Invalid request', 'points must be 2 to 5000'),
      ),
    )
    renderApp('/activities/1/compare/2')
    expect(await screen.findByRole('alert')).toHaveTextContent('points must be 2 to 5000')
  })

  it('shows a missing activity', async () => {
    server.use(http.get('/api/activities/:id', () => problem(404, 'not-found', 'Not found')))
    renderApp('/activities/1/compare/3')
    expect(await screen.findByRole('alert')).toHaveTextContent('Not found')
  })
})
