import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { Gear, GearInput } from '@/api/gear'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const trainers: Gear = {
  id: 5,
  kind: 'shoes',
  name: 'Trainers',
  replacementDistanceM: 800000,
  retired: false,
  distanceM: 812345,
  timeS: 270000,
  activities: 81,
  replacementDue: true,
}
const bike: Gear = {
  id: 6,
  kind: 'bike',
  name: 'Road bike',
  retired: false,
  distanceM: 40000,
  timeS: 5400,
  activities: 1,
  replacementDue: false,
}

function gearReturns(items: Gear[], nextCursor?: string) {
  server.use(http.get('/api/gear', () => HttpResponse.json({ items, nextCursor })))
}

beforeEach(() => {
  loggedInAs(ann)
})

describe('GearPage', () => {
  it('lists gear with totals and warns about gear due for replacement', async () => {
    gearReturns([trainers, bike])
    renderApp('/gear')
    const row = (await screen.findByRole('cell', { name: 'Trainers' })).closest('tr')
    if (!row) throw new Error('no row')
    expect(within(row).getByRole('cell', { name: '812.35 km' })).toBeInTheDocument()
    expect(within(row).getByRole('cell', { name: '75:00:00' })).toBeInTheDocument()
    expect(within(row).getByRole('cell', { name: '800.00 km' })).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Time to replace: Trainers.')
  })

  it('does not warn about retired gear', async () => {
    gearReturns([{ ...trainers, retired: true }])
    renderApp('/gear')
    expect(await screen.findByText('(retired)')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows the empty state', async () => {
    gearReturns([])
    renderApp('/gear')
    expect(await screen.findByText(/No gear yet/)).toBeInTheDocument()
  })

  it('reports load errors', async () => {
    server.use(http.get('/api/gear', () => problem(500, 'internal', 'Internal error')))
    renderApp('/gear')
    expect(await screen.findByRole('alert')).toHaveTextContent('Internal error')
  })

  it('loads more pages with the cursor', async () => {
    server.use(
      http.get('/api/gear', ({ request }) =>
        new URL(request.url).searchParams.get('cursor') === 'next'
          ? HttpResponse.json({ items: [bike] })
          : HttpResponse.json({ items: [trainers], nextCursor: 'next' }),
      ),
    )
    renderApp('/gear')
    await userEvent.click(await screen.findByRole('button', { name: 'Show more' }))
    expect(await screen.findByRole('cell', { name: 'Road bike' })).toBeInTheDocument()
  })

  it('adds gear with a replacement distance and the CSRF token', async () => {
    gearReturns([])
    let body: GearInput | undefined
    let csrf: string | null = null
    server.use(
      http.post('/api/gear', async ({ request }) => {
        body = (await request.json()) as GearInput
        csrf = request.headers.get('X-CSRF-Token')
        gearReturns([bike])
        return HttpResponse.json(bike, { status: 201 })
      }),
    )
    renderApp('/gear')
    await userEvent.click(await screen.findByRole('button', { name: 'Add gear' }))
    const dialog = screen.getByRole('dialog', { name: 'Add gear' })
    await userEvent.type(within(dialog).getByLabelText('Name'), ' Road bike ')
    await userEvent.selectOptions(within(dialog).getByLabelText('Kind'), 'Bike')
    await userEvent.type(within(dialog).getByLabelText('Replace at (km, optional)'), '15000,5')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('cell', { name: 'Road bike' })).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(body).toEqual({
      name: 'Road bike',
      kind: 'bike',
      retired: false,
      replacementDistanceM: 15000500,
    })
    expect(csrf).toBe('csrf-ann')
  })

  it.each([
    ['', '', 'Enter a name.'],
    ['Shoes', 'far', 'Enter the replacement distance in kilometres, e.g. 800.'],
    ['Shoes', '0', 'Enter the replacement distance in kilometres, e.g. 800.'],
  ])('rejects name %j and distance %j', async (name, distance, message) => {
    gearReturns([])
    renderApp('/gear')
    await userEvent.click(await screen.findByRole('button', { name: 'Add gear' }))
    const dialog = screen.getByRole('dialog', { name: 'Add gear' })
    if (name) await userEvent.type(within(dialog).getByLabelText('Name'), name)
    if (distance) {
      await userEvent.type(within(dialog).getByLabelText('Replace at (km, optional)'), distance)
    }
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(within(dialog).getByRole('alert')).toHaveTextContent(message)
  })

  it('edits and retires gear, keeping its fields', async () => {
    gearReturns([trainers])
    let body: GearInput | undefined
    let id: unknown
    server.use(
      http.put('/api/gear/:id', async ({ request, params }) => {
        id = params.id
        body = (await request.json()) as GearInput
        return HttpResponse.json({ ...trainers, retired: true })
      }),
    )
    renderApp('/gear')
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Trainers' }))
    const dialog = screen.getByRole('dialog', { name: 'Edit Trainers' })
    expect(within(dialog).getByLabelText('Replace at (km, optional)')).toHaveValue('800')
    await userEvent.selectOptions(within(dialog).getByLabelText('Status'), 'Retired')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await vi.waitFor(() => {
      expect(body).toEqual({
        name: 'Trainers',
        kind: 'shoes',
        retired: true,
        replacementDistanceM: 800000,
      })
    })
    expect(id).toBe('5')
  })

  it('shows save errors', async () => {
    gearReturns([])
    server.use(
      http.post('/api/gear', () =>
        problem(400, 'invalid-request', 'Invalid request', 'name must be 1 to 80 characters'),
      ),
    )
    renderApp('/gear')
    await userEvent.click(await screen.findByRole('button', { name: 'Add gear' }))
    const dialog = screen.getByRole('dialog', { name: 'Add gear' })
    await userEvent.type(within(dialog).getByLabelText('Name'), 'x')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent(
      'name must be 1 to 80 characters',
    )
  })

  it('deletes gear after confirming', async () => {
    gearReturns([trainers, bike])
    let deleted: unknown
    server.use(
      http.delete('/api/gear/:id', ({ params }) => {
        deleted = params.id
        gearReturns([bike])
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderApp('/gear')
    await userEvent.click(await screen.findByRole('button', { name: 'Delete Trainers' }))
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(deleted).toBeUndefined()

    await userEvent.click(screen.getByRole('button', { name: 'Delete Trainers' }))
    await userEvent.click(screen.getByRole('button', { name: 'Delete Trainers' }))
    await vi.waitFor(() => {
      expect(screen.queryByRole('cell', { name: 'Trainers' })).not.toBeInTheDocument()
    })
    expect(deleted).toBe('5')
  })

  it('reports delete errors', async () => {
    gearReturns([bike])
    server.use(http.delete('/api/gear/:id', () => problem(404, 'not-found', 'Not found')))
    renderApp('/gear')
    await userEvent.click(await screen.findByRole('button', { name: 'Delete Road bike' }))
    await userEvent.click(screen.getByRole('button', { name: 'Delete Road bike' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Not found')
  })
})
