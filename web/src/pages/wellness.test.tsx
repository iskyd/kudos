import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { HealthLogEntry, HealthLogInput, WellnessDay, WellnessDayInput } from '@/api/wellness'
import { toISODay } from '@/lib/format/date'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const today = toISODay(new Date())
const checkIn: WellnessDay = {
  day: '2026-10-01',
  soreness: 2,
  sleepQuality: 4,
  motivation: 5,
  weightKg: 70.46,
  note: 'Legs heavy',
}
const knee: HealthLogEntry = {
  id: 3,
  kind: 'injury',
  description: 'Sore left knee',
  startDate: '2026-09-01',
  endDate: '2026-09-20',
}
const cold: HealthLogEntry = {
  id: 4,
  kind: 'illness',
  description: 'Cold',
  startDate: '2026-10-10',
}

function returns(days: WellnessDay[], log: HealthLogEntry[]) {
  server.use(
    http.get('/api/wellness', () => HttpResponse.json({ items: days })),
    http.get('/api/health-log', () => HttpResponse.json({ items: log })),
  )
}

beforeEach(() => {
  loggedInAs(ann)
  localStorage.clear()
})

describe('WellnessPage', () => {
  it('lists check-ins and the health log', async () => {
    returns([checkIn], [cold, knee])
    renderApp('/wellness')
    const row = (await screen.findByRole('cell', { name: '1 October 2026' })).closest('tr')
    if (!row) throw new Error('no row')
    expect(within(row).getByRole('cell', { name: '70.5 kg' })).toBeInTheDocument()
    expect(within(row).getByRole('cell', { name: 'Legs heavy' })).toBeInTheDocument()
    expect(within(row).getAllByRole('cell', { name: '—' })).toHaveLength(3)
    const coldRow = screen.getByRole('cell', { name: 'Cold' }).closest('tr')
    if (!coldRow) throw new Error('no row')
    expect(within(coldRow).getByRole('cell', { name: 'Ongoing' })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: '20 September 2026' })).toBeInTheDocument()
  })

  it('shows the empty states', async () => {
    returns([], [])
    renderApp('/wellness')
    expect(await screen.findByText('No check-ins yet.')).toBeInTheDocument()
    expect(await screen.findByText(/Nothing logged/)).toBeInTheDocument()
  })

  it('reports load errors', async () => {
    server.use(
      http.get('/api/wellness', () => problem(500, 'internal', 'Internal error')),
      http.get('/api/health-log', () => problem(500, 'internal', 'Log down')),
    )
    renderApp('/wellness')
    expect(await screen.findByText('Internal error')).toBeInTheDocument()
    expect(await screen.findByText('Log down')).toBeInTheDocument()
  })

  it("saves today's check-in with the CSRF token", async () => {
    returns([], [])
    let body: WellnessDayInput | undefined
    let path = ''
    let csrf: string | null = null
    server.use(
      http.put('/api/wellness/:day', async ({ request, params }) => {
        body = (await request.json()) as WellnessDayInput
        path = String(params.day)
        csrf = request.headers.get('X-CSRF-Token')
        return HttpResponse.json({ day: path, ...body })
      }),
    )
    renderApp('/wellness')
    await userEvent.selectOptions(await screen.findByLabelText('Soreness'), '3')
    await userEvent.selectOptions(screen.getByLabelText('Motivation'), '4')
    await userEvent.type(screen.getByLabelText('Weight (kg)'), '70,5')
    await userEvent.type(screen.getByLabelText('Note'), ' Tired ')
    await userEvent.click(screen.getByRole('button', { name: /^Save / }))
    expect(await screen.findByText('Saved.')).toBeInTheDocument()
    expect(path).toBe(today)
    expect(body).toEqual({ soreness: 3, motivation: 4, weightKg: 70.5, note: 'Tired' })
    expect(csrf).toBe('csrf-ann')
  })

  it('edits a listed day starting from its values', async () => {
    returns([checkIn], [])
    let path = ''
    let body: WellnessDayInput | undefined
    server.use(
      http.put('/api/wellness/:day', async ({ request, params }) => {
        body = (await request.json()) as WellnessDayInput
        path = String(params.day)
        return HttpResponse.json({ day: path, ...body })
      }),
    )
    renderApp('/wellness')
    await userEvent.click(await screen.findByRole('button', { name: 'Edit 1 October 2026' }))
    expect(screen.getByLabelText('Day')).toHaveValue('2026-10-01')
    expect(screen.getByLabelText('Sleep quality')).toHaveValue('4')
    expect(screen.getByLabelText('Weight (kg)')).toHaveValue('70.46')
    await userEvent.selectOptions(screen.getByLabelText('Stress'), '2')
    await userEvent.click(screen.getByRole('button', { name: 'Save 1 October 2026' }))
    await screen.findByText('Saved.')
    expect(path).toBe('2026-10-01')
    expect(body).toEqual({
      soreness: 2,
      sleepQuality: 4,
      stress: 2,
      motivation: 5,
      weightKg: 70.46,
      note: 'Legs heavy',
    })
  })

  it.each([
    ['', 'Enter at least one value.'],
    ['heavy', 'Enter weight (kg) as a number.'],
  ])('rejects weight %j', async (weight, message) => {
    returns([], [])
    renderApp('/wellness')
    if (weight) await userEvent.type(await screen.findByLabelText('Weight (kg)'), weight)
    await userEvent.click(await screen.findByRole('button', { name: /^Save / }))
    expect(await screen.findByRole('alert')).toHaveTextContent(message)
  })

  it('deletes a day', async () => {
    returns([checkIn], [])
    let deleted = ''
    server.use(
      http.delete('/api/wellness/:day', ({ params }) => {
        deleted = String(params.day)
        returns([], [])
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderApp('/wellness')
    await userEvent.click(await screen.findByRole('button', { name: 'Delete 1 October 2026' }))
    expect(await screen.findByText('No check-ins yet.')).toBeInTheDocument()
    expect(deleted).toBe('2026-10-01')
  })

  it('loads more days with the cursor', async () => {
    server.use(
      http.get('/api/wellness', ({ request }) =>
        new URL(request.url).searchParams.get('cursor') === 'next'
          ? HttpResponse.json({ items: [checkIn] })
          : HttpResponse.json({ items: [{ day: '2026-10-02', stress: 1 }], nextCursor: 'next' }),
      ),
      http.get('/api/health-log', () => HttpResponse.json({ items: [] })),
    )
    renderApp('/wellness')
    await userEvent.click(await screen.findByRole('button', { name: 'Show more days' }))
    expect(await screen.findByRole('cell', { name: '1 October 2026' })).toBeInTheDocument()
  })

  it('logs an injury', async () => {
    returns([], [])
    let body: HealthLogInput | undefined
    server.use(
      http.post('/api/health-log', async ({ request }) => {
        body = (await request.json()) as HealthLogInput
        returns([], [knee])
        return HttpResponse.json({ id: 3, ...body }, { status: 201 })
      }),
    )
    renderApp('/wellness')
    await userEvent.click(await screen.findByRole('button', { name: 'Log an entry' }))
    const dialog = screen.getByRole('dialog', { name: 'Log an entry' })
    expect(within(dialog).queryByRole('option', { name: 'Period' })).not.toBeInTheDocument()
    await userEvent.type(within(dialog).getByLabelText('Description'), ' Sore left knee ')
    await userEvent.type(within(dialog).getByLabelText('Start'), '2026-09-01')
    await userEvent.type(within(dialog).getByLabelText('End (optional)'), '2026-09-20')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('cell', { name: 'Sore left knee' })).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(body).toEqual({
      kind: 'injury',
      description: 'Sore left knee',
      startDate: '2026-09-01',
      endDate: '2026-09-20',
    })
  })

  it.each([
    ['injury', '', '2026-10-01', '', 'Describe the injury or illness.'],
    ['illness', 'Flu', '', '', 'Enter the start date.'],
    ['illness', 'Flu', '2026-10-02', '2026-10-01', 'The end date must not be before'],
  ])('rejects %s %j from %j to %j', async (kind, description, start, end, message) => {
    returns([], [])
    renderApp('/wellness')
    await userEvent.click(await screen.findByRole('button', { name: 'Log an entry' }))
    const dialog = screen.getByRole('dialog', { name: 'Log an entry' })
    await userEvent.selectOptions(within(dialog).getByLabelText('Kind'), kind)
    if (description) await userEvent.type(within(dialog).getByLabelText('Description'), description)
    if (start) await userEvent.type(within(dialog).getByLabelText('Start'), start)
    if (end) await userEvent.type(within(dialog).getByLabelText('End (optional)'), end)
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(within(dialog).getByRole('alert')).toHaveTextContent(message)
  })

  it('logs a period without a description once cycle tracking is on, and remembers it', async () => {
    returns([], [])
    let body: HealthLogInput | undefined
    server.use(
      http.post('/api/health-log', async ({ request }) => {
        body = (await request.json()) as HealthLogInput
        return HttpResponse.json({ id: 9, ...body }, { status: 201 })
      }),
    )
    renderApp('/wellness')
    await userEvent.click(await screen.findByRole('checkbox', { name: 'Track menstrual cycle' }))
    expect(localStorage.getItem('kudos.cycleTracking')).toBe('1')
    await userEvent.click(screen.getByRole('button', { name: 'Log an entry' }))
    const dialog = screen.getByRole('dialog', { name: 'Log an entry' })
    await userEvent.selectOptions(within(dialog).getByLabelText('Kind'), 'period')
    await userEvent.type(within(dialog).getByLabelText('Start'), '2026-10-12')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await vi.waitFor(() => {
      expect(body).toEqual({ kind: 'period', startDate: '2026-10-12' })
    })
    await userEvent.click(screen.getByRole('checkbox', { name: 'Track menstrual cycle' }))
    expect(localStorage.getItem('kudos.cycleTracking')).toBeNull()
  })

  it('edits and deletes a log entry', async () => {
    returns([], [cold])
    let put: HealthLogInput | undefined
    let deleted = 0
    server.use(
      http.put('/api/health-log/:id', async ({ request }) => {
        put = (await request.json()) as HealthLogInput
        returns([], [{ ...cold, ...put }])
        return HttpResponse.json({ ...cold, ...put })
      }),
      http.delete('/api/health-log/:id', ({ params }) => {
        deleted = Number(params.id)
        returns([], [])
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderApp('/wellness')
    await userEvent.click(
      await screen.findByRole('button', { name: 'Edit Illness from 10 October 2026' }),
    )
    const dialog = screen.getByRole('dialog', { name: 'Edit illness' })
    expect(within(dialog).getByLabelText('Description')).toHaveValue('Cold')
    await userEvent.type(within(dialog).getByLabelText('End (optional)'), '2026-10-14')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('cell', { name: '14 October 2026' })).toBeInTheDocument()
    expect(put).toEqual({
      kind: 'illness',
      description: 'Cold',
      startDate: '2026-10-10',
      endDate: '2026-10-14',
    })
    await userEvent.click(
      screen.getByRole('button', { name: 'Delete Illness from 10 October 2026' }),
    )
    expect(await screen.findByText(/Nothing logged/)).toBeInTheDocument()
    expect(deleted).toBe(4)
  })
})
