import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import type { ActivitySummary } from '@/api/activities'
import type { PlannedWorkout, PlannedWorkoutLinkInput } from '@/api/plans'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const track: PlannedWorkout = {
  id: 7,
  planId: 1,
  link: 'auto',
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

  describe('completion', () => {
    const morning: ActivitySummary = {
      id: 30,
      sport: 'running',
      subtype: 'outdoor',
      source: 'upload',
      startTime: '2026-11-12T06:00:00Z',
      utcOffsetS: 3600,
      timerTimeS: 1800,
    }
    const evening = { ...morning, id: 31, startTime: '2026-11-12T17:30:00Z', timerTimeS: 3600 }

    function serve(w: PlannedWorkout, sent: (PlannedWorkoutLinkInput | 'auto')[]) {
      let current = w
      server.use(
        http.get('/api/planned-workouts/:id', () => HttpResponse.json(current)),
        http.get('/api/activities', ({ request }) => {
          const p = new URL(request.url).searchParams
          return HttpResponse.json({
            items:
              p.get('from') === '2026-11-12' && p.get('to') === '2026-11-12'
                ? [morning, evening]
                : [],
          })
        }),
        http.put('/api/planned-workouts/7/link', async ({ request }) => {
          const body = (await request.json()) as PlannedWorkoutLinkInput
          sent.push(body)
          if (body.activityId === 99) {
            return problem(409, 'activity-linked', 'Activity already linked', 'already linked')
          }
          current = { ...current, link: 'manual', activityId: body.activityId }
          return HttpResponse.json(current)
        }),
        http.delete('/api/planned-workouts/7/link', () => {
          sent.push('auto')
          current = { ...current, link: 'auto', activityId: 30 }
          return HttpResponse.json(current)
        }),
      )
    }

    it('shows the matched activity and unlinks it', async () => {
      const sent: (PlannedWorkoutLinkInput | 'auto')[] = []
      serve({ ...track, activityId: 30 }, sent)
      renderApp('/planned/7')
      const section = await screen.findByRole('region', { name: 'Completion' })
      expect(within(section).getByText(/matched automatically/)).toBeInTheDocument()
      expect(within(section).getByRole('link', { name: 'this activity' })).toHaveAttribute(
        'href',
        '/activities/30',
      )
      await userEvent.setup().click(within(section).getByRole('button', { name: 'Unlink' }))
      expect(
        await within(section).findByText('Unlinked: no activity completes it.'),
      ).toBeInTheDocument()
      expect(sent).toEqual([{}])
    })

    it('links one of the day’s activities, and goes back to automatic matching', async () => {
      const sent: (PlannedWorkoutLinkInput | 'auto')[] = []
      serve(track, sent)
      renderApp('/planned/7')
      const user = userEvent.setup()
      const section = await screen.findByRole('region', { name: 'Completion' })
      expect(within(section).getByText('Not completed yet.')).toBeInTheDocument()
      expect(within(section).queryByRole('button', { name: 'Match automatically' })).toBeNull()
      const select = await within(section).findByLabelText('Activity of the day')
      expect(
        within(select).getByRole('option', { name: '18:30 · Running · 1:00:00' }),
      ).toBeInTheDocument()
      await user.selectOptions(select, '07:00 · Running · 30:00')
      await user.click(within(section).getByRole('button', { name: 'Link' }))
      expect(await within(section).findByText(/linked by you/)).toBeInTheDocument()

      await user.click(within(section).getByRole('button', { name: 'Match automatically' }))
      expect(await within(section).findByText(/matched automatically/)).toBeInTheDocument()
      expect(sent).toEqual([{ activityId: 30 }, 'auto'])
    })

    it('shows link failures', async () => {
      const sent: (PlannedWorkoutLinkInput | 'auto')[] = []
      serve(track, sent)
      server.use(
        http.get('/api/activities', () => HttpResponse.json({ items: [{ ...morning, id: 99 }] })),
      )
      renderApp('/planned/7')
      const user = userEvent.setup()
      const section = await screen.findByRole('region', { name: 'Completion' })
      await user.selectOptions(
        await within(section).findByLabelText('Activity of the day'),
        '07:00 · Running · 30:00',
      )
      await user.click(within(section).getByRole('button', { name: 'Link' }))
      expect(await within(section).findByRole('alert')).toHaveTextContent('already linked')
    })
  })
})
