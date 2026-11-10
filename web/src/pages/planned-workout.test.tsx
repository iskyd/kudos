import { screen, within } from '@testing-library/react'
import { delay, http, HttpResponse } from 'msw'
import type { PlannedWorkout } from '@/api/plans'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const track: PlannedWorkout = {
  id: 7,
  planId: 1,
  day: '2026-11-12',
  sport: 'running',
  title: '5 × 1 km',
  description: 'Track session',
  steps: [
    {
      kind: 'warmup',
      durationType: 'time',
      duration: 900,
      target: { metric: 'heart_rate', zone: 2 },
    },
    {
      kind: 'repeat',
      count: 5,
      steps: [
        {
          kind: 'interval',
          durationType: 'distance',
          duration: 1000,
          target: { metric: 'pace', low: 1000 / 256, high: 1000 / 244 },
        },
        {
          kind: 'repeat',
          count: 2,
          steps: [{ kind: 'recovery', durationType: 'time', duration: 45 }],
        },
      ],
    },
    { kind: 'cooldown', durationType: 'open', note: 'Easy jog home' },
  ],
}

describe('PlannedWorkoutPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it('shows loading, then a failure', async () => {
    server.use(http.get('/api/planned-workouts/:id', () => delay('infinite')))
    renderApp('/planned/7')
    expect(await screen.findByText('Loading…')).toBeInTheDocument()

    server.use(
      http.get('/api/planned-workouts/:id', () =>
        problem(404, 'not-found', 'Not found', 'no such planned workout'),
      ),
    )
    renderApp('/planned/8')
    expect(await screen.findByRole('alert')).toHaveTextContent('no such planned workout')
  })

  it('shows the workout and its steps, repeats nested', async () => {
    server.use(
      http.get('/api/planned-workouts/:id', ({ params }) =>
        params.id === '7' ? HttpResponse.json(track) : problem(404, 'not-found', 'Not found'),
      ),
    )
    renderApp('/planned/7')
    expect(await screen.findByRole('heading', { name: '5 × 1 km' })).toBeInTheDocument()
    expect(screen.getByText('Planned for 12 November 2026 · Running')).toBeInTheDocument()
    expect(screen.getByText('Track session')).toBeInTheDocument()

    const steps = screen.getByRole('region', { name: 'Steps' })
    expect(within(steps).getByText('Warm-up · 15:00 · HR zone 2')).toBeInTheDocument()
    const five = within(steps).getByRole('listitem', { name: '5 times' })
    expect(within(five).getByText('Interval · 1.00 km · 4:04–4:16 /km')).toBeInTheDocument()
    const two = within(five).getByRole('listitem', { name: '2 times' })
    expect(within(two).getByText('Recovery · 0:45')).toBeInTheDocument()
    expect(within(steps).getByText('Cool-down · until the lap button')).toBeInTheDocument()
    expect(within(steps).getByText('Easy jog home')).toBeInTheDocument()
  })
})
