import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import type { Goal, GoalInput } from '@/api/goals'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const yearGoal: Goal = {
  id: 2,
  period: 'year',
  start: '2026-01-01',
  end: '2026-12-31',
  sport: 'running',
  metric: 'distance',
  target: 2_000_000,
  value: 1_500_000,
  projected: 1_800_000,
  status: 'behind',
}

const monthGoal: Goal = {
  id: 1,
  period: 'month',
  start: '2026-10-01',
  end: '2026-10-31',
  metric: 'duration',
  target: 36_000,
  value: 40_000,
  projected: 40_000,
  status: 'achieved',
}

describe('GoalsPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it('shows loading, then a failure', async () => {
    server.use(http.get('/api/goals', () => delay('infinite')))
    renderApp('/goals')
    expect(await screen.findByText('Loading…')).toBeInTheDocument()

    server.use(http.get('/api/goals', () => problem(500, 'internal', 'Something went wrong')))
    renderApp('/goals')
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong')
  })

  it('shows an empty state without goals', async () => {
    renderApp('/goals')
    expect(await screen.findByText(/No goals yet/)).toBeInTheDocument()
  })

  it('shows each goal with its progress and projection', async () => {
    const ahead: Goal = {
      ...yearGoal,
      id: 3,
      sport: 'cycling',
      target: 100_000,
      value: 40_000,
      projected: 120_000,
      projectedFinish: '2026-11-25',
      status: 'ahead',
    }
    server.use(
      http.get('/api/goals', () => HttpResponse.json({ items: [ahead, yearGoal, monthGoal] })),
    )
    renderApp('/goals')

    const running = within(
      await screen.findByRole('listitem', { name: '2000.00 km running, 2026' }),
    )
    expect(running.getByText('Behind')).toBeInTheDocument()
    expect(running.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '75')
    expect(running.getByText(/1500\.00 km of 2000\.00 km/)).toBeInTheDocument()
    expect(running.getByText('Projected 1800.00 km by 31 December 2026.')).toBeInTheDocument()

    const cycling = within(screen.getByRole('listitem', { name: '100.00 km cycling, 2026' }))
    expect(cycling.getByText('On track')).toBeInTheDocument()
    expect(
      cycling.getByText(
        'Projected 120.00 km by 31 December 2026, reaching the target on 25 November 2026.',
      ),
    ).toBeInTheDocument()

    // Past the target the bar stays full and nothing is projected.
    const month = within(
      screen.getByRole('listitem', { name: '10:00:00 in all sports, October 2026' }),
    )
    expect(month.getByText('Achieved')).toBeInTheDocument()
    expect(month.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '100')
    expect(month.queryByText(/Projected/)).not.toBeInTheDocument()
  })

  it('adds a goal', async () => {
    const sent: GoalInput[] = []
    server.use(
      http.post('/api/goals', async ({ request }) => {
        sent.push((await request.json()) as GoalInput)
        return HttpResponse.json(yearGoal, { status: 201 })
      }),
    )
    renderApp('/goals')
    await userEvent.click(await screen.findByRole('button', { name: 'Add goal' }))
    const dialog = within(await screen.findByRole('dialog', { name: 'Add goal' }))

    await userEvent.click(dialog.getByRole('button', { name: 'Save' }))
    expect(dialog.getByRole('alert')).toHaveTextContent('Enter a target in km.')

    await userEvent.clear(dialog.getByLabelText('Any day in the period'))
    await userEvent.type(dialog.getByLabelText('Any day in the period'), '2026-06-15')
    await userEvent.selectOptions(dialog.getByLabelText('Sport'), 'running')
    await userEvent.type(dialog.getByLabelText('Target (km)'), '2000')
    await userEvent.click(dialog.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText(/No goals yet/)).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(sent).toEqual([
      {
        period: 'year',
        start: '2026-06-15',
        sport: 'running',
        metric: 'distance',
        target: 2_000_000,
      },
    ])
  })

  it('reads a time target and shows a rejected goal', async () => {
    const sent: GoalInput[] = []
    server.use(
      http.post('/api/goals', async ({ request }) => {
        sent.push((await request.json()) as GoalInput)
        return problem(400, 'invalid_request', 'unknown sport')
      }),
    )
    renderApp('/goals')
    await userEvent.click(await screen.findByRole('button', { name: 'Add goal' }))
    const dialog = within(await screen.findByRole('dialog', { name: 'Add goal' }))
    await userEvent.selectOptions(dialog.getByLabelText('Period'), 'week')
    await userEvent.selectOptions(dialog.getByLabelText('Measure'), 'duration')
    await userEvent.type(dialog.getByLabelText('Target (h:mm:ss or minutes)'), '5:00:00')
    await userEvent.click(dialog.getByRole('button', { name: 'Save' }))
    expect(await dialog.findByRole('alert')).toHaveTextContent('unknown sport')
    expect(sent[0]).toMatchObject({ period: 'week', metric: 'duration', target: 18_000 })
    expect(sent[0]).not.toHaveProperty('sport')
  })

  it('deletes a goal', async () => {
    let goals = [yearGoal]
    server.use(
      http.get('/api/goals', () => HttpResponse.json({ items: goals })),
      http.delete('/api/goals/:id', ({ params }) => {
        goals = goals.filter((g) => String(g.id) !== params.id)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderApp('/goals')
    await userEvent.click(
      await screen.findByRole('button', { name: 'Delete 2000.00 km running, 2026' }),
    )
    expect(await screen.findByText(/No goals yet/)).toBeInTheDocument()
  })
})
