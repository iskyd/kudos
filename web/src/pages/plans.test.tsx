import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { PlanDecisions, PlannedWorkoutInput, PlanProposal } from '@/api/plans'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const easy: PlannedWorkoutInput = {
  day: '2026-11-13',
  sport: 'running',
  title: 'Easy run',
  steps: [{ kind: 'recovery', durationType: 'time', duration: 2700 }],
}
const tempo: PlannedWorkoutInput = {
  ...easy,
  title: 'Tempo',
  description: 'Steady, not hard',
  steps: [
    { kind: 'warmup', durationType: 'time', duration: 900 },
    {
      kind: 'interval',
      durationType: 'time',
      duration: 1200,
      target: { metric: 'heart_rate', zone: 4 },
    },
  ],
}
const ride: PlannedWorkoutInput = { ...easy, day: '2026-11-15', sport: 'cycling', title: 'Ride' }
const long: PlannedWorkoutInput = { ...easy, day: '2026-11-16', title: 'Long run' }

const base: PlanProposal = {
  id: 4,
  status: 'open',
  createdAt: '2026-11-12T08:00:00Z',
  from: '2026-11-13',
  to: '2026-11-26',
  items: [],
}
const open: PlanProposal = {
  ...base,
  items: [
    { id: 1, action: 'change', status: 'pending', workoutId: 7, before: easy, after: tempo },
    { id: 2, action: 'remove', status: 'pending', workoutId: 8, before: ride },
    { id: 3, action: 'add', status: 'pending', after: long },
  ],
}

function latest(p: PlanProposal) {
  server.use(http.get('/api/plan-proposals/latest', () => HttpResponse.json(p)))
}

describe('PlansPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it('explains plans before the first, and shows load failures', async () => {
    renderApp('/plans')
    expect(await screen.findByText(/No plan yet/)).toBeInTheDocument()

    server.use(
      http.get('/api/plan-proposals/latest', () => problem(500, 'internal', 'Internal error')),
    )
    renderApp('/plans')
    expect(await screen.findByRole('alert')).toHaveTextContent('Internal error')
  })

  it('asks for a plan and shows it being generated', async () => {
    const tokens: (string | null)[] = []
    server.use(
      http.post('/api/plan-proposals', ({ request }) => {
        tokens.push(request.headers.get('X-CSRF-Token'))
        return HttpResponse.json({ ...base, status: 'generating' }, { status: 202 })
      }),
    )
    renderApp('/plans')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Plan the next 14 days' }))
    expect(await screen.findByRole('status')).toHaveTextContent(
      'The coach is planning 13 November 2026 to 26 November 2026…',
    )
    expect(screen.getByRole('button', { name: 'Plan the next 14 days' })).toBeDisabled()
    expect(tokens).toEqual(['csrf-ann'])
  })

  it('says why a plan could not be asked for or made', async () => {
    server.use(
      http.post('/api/plan-proposals', () =>
        problem(409, 'llm-not-configured', 'No AI coach provider set up', 'Set one up first'),
      ),
    )
    renderApp('/plans')
    await userEvent
      .setup()
      .click(await screen.findByRole('button', { name: 'Plan the next 14 days' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Set one up first')

    latest({ ...base, status: 'failed', failure: 'invalid_output' })
    renderApp('/plans')
    expect(
      await screen.findByText(
        'The coach’s plan was invalid twice, so nothing was proposed. Try again.',
      ),
    ).toBeInTheDocument()
  })

  it('says when the coach proposed no change', async () => {
    latest({ ...base, status: 'closed' })
    renderApp('/plans')
    expect(
      await screen.findByText(
        'The coach proposed no changes for 13 November 2026 to 26 November 2026.',
      ),
    ).toBeInTheDocument()
  })

  it('shows each change as a diff', async () => {
    latest(open)
    renderApp('/plans')
    const changed = await screen.findByRole('listitem', { name: 'Changed · 13 November 2026' })
    const before = within(changed).getByRole('group', { name: 'Before' })
    const after = within(changed).getByRole('group', { name: 'After' })
    expect(within(before).getByText('Easy run · Running')).toBeInTheDocument()
    expect(within(before).getByText('Recovery · 45:00')).toBeInTheDocument()
    expect(within(after).getByText('Tempo · Running')).toBeInTheDocument()
    expect(within(after).getByText('Steady, not hard')).toBeInTheDocument()
    expect(within(after).getByText('Interval · 20:00 · HR zone 4')).toBeInTheDocument()

    const removed = screen.getByRole('listitem', { name: 'Removed · 15 November 2026' })
    expect(within(removed).getByRole('group', { name: 'Before' })).toHaveTextContent(
      'Ride · Cycling',
    )
    expect(within(removed).queryByRole('group', { name: 'After' })).not.toBeInTheDocument()
    const added = screen.getByRole('listitem', { name: 'Added · 16 November 2026' })
    expect(within(added).getByRole('group', { name: 'New' })).toHaveTextContent('Long run')
    expect(within(added).queryByRole('group', { name: 'Before' })).not.toBeInTheDocument()
  })

  it('approves and rejects items one at a time or all together', async () => {
    const sent: PlanDecisions[] = []
    let current = open
    latest(open)
    server.use(
      http.post('/api/plan-proposals/4/decisions', async ({ request }) => {
        const body = (await request.json()) as PlanDecisions
        sent.push(body)
        const items = current.items.map((it) =>
          body.approve.includes(it.id)
            ? { ...it, status: 'approved' as const }
            : body.reject.includes(it.id)
              ? { ...it, status: 'rejected' as const }
              : it,
        )
        const done = items.every((it) => it.status !== 'pending')
        current = { ...current, items, status: done ? 'closed' : 'open' }
        return HttpResponse.json(current)
      }),
    )
    renderApp('/plans')
    const user = userEvent.setup()
    const added = await screen.findByRole('listitem', { name: 'Added · 16 November 2026' })
    await user.click(within(added).getByRole('button', { name: 'Approve' }))
    expect(await within(added).findByText('Approved')).toBeInTheDocument()

    const removed = screen.getByRole('listitem', { name: 'Removed · 15 November 2026' })
    await user.click(within(removed).getByRole('button', { name: 'Reject' }))
    expect(await within(removed).findByText('Rejected')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Approve all' }))
    const changed = screen.getByRole('listitem', { name: 'Changed · 13 November 2026' })
    expect(await within(changed).findByText('Approved')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Approve all' })).not.toBeInTheDocument()
    expect(sent).toEqual([
      { approve: [3], reject: [] },
      { approve: [], reject: [2] },
      { approve: [1], reject: [] },
    ])
  })

  it('rejects everything pending at once, and shows decision failures', async () => {
    const sent: PlanDecisions[] = []
    latest(open)
    server.use(
      http.post('/api/plan-proposals/4/decisions', async ({ request }) => {
        sent.push((await request.json()) as PlanDecisions)
        return problem(
          409,
          'proposal-closed',
          'Plan proposal closed',
          'the proposal is no longer open',
        )
      }),
    )
    renderApp('/plans')
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Reject all' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('the proposal is no longer open')
    expect(sent).toEqual([{ approve: [], reject: [1, 2, 3] }])
  })

  it('shows a superseded proposal without decisions', async () => {
    latest({ ...open, status: 'superseded' })
    renderApp('/plans')
    expect(await screen.findByText('A newer proposal replaced this one.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Approve' })).not.toBeInTheDocument()
    expect(screen.getAllByText('Not decided')).toHaveLength(3)
  })
})
