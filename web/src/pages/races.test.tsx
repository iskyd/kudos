import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import type { Race, RaceInput } from '@/api/races'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const marathon: Race = {
  id: 1,
  name: 'City Marathon',
  day: '2026-12-06',
  sport: 'running',
  distanceM: 42195,
  goalTimeS: 11700,
  priority: 'A',
}
const tenK: Race = {
  id: 2,
  name: 'Park 10K',
  day: '2026-11-15',
  sport: 'running',
  distanceM: 10000,
  priority: 'C',
}

describe('RacesPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it('is in the navigation', async () => {
    renderApp('/')
    expect(await screen.findByRole('link', { name: 'Races' })).toHaveAttribute('href', '/races')
  })

  it('shows loading, then a failure', async () => {
    server.use(http.get('/api/races', () => delay('infinite')))
    renderApp('/races')
    expect(await screen.findByText('Loading…')).toBeInTheDocument()

    server.use(http.get('/api/races', () => problem(500, 'internal', 'Something went wrong')))
    renderApp('/races')
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong')
  })

  it('says when there are no races', async () => {
    renderApp('/races')
    expect(await screen.findByText(/No races yet/)).toBeInTheDocument()
  })

  it('lists races with their goal and priority', async () => {
    server.use(http.get('/api/races', () => HttpResponse.json({ items: [marathon, tenK] })))
    renderApp('/races')
    const m = within(await screen.findByRole('listitem', { name: 'City Marathon' }))
    expect(m.getByText('6 December 2026 · Running · 42.20 km')).toBeInTheDocument()
    expect(m.getByText('Goal 3:15:00')).toBeInTheDocument()
    expect(m.getByText('Priority A')).toBeInTheDocument()
    expect(
      within(screen.getByRole('listitem', { name: 'Park 10K' })).getByText('No goal time'),
    ).toBeInTheDocument()
  })

  it('adds a race in display units', async () => {
    const sent: RaceInput[] = []
    server.use(
      http.post('/api/races', async ({ request }) => {
        sent.push((await request.json()) as RaceInput)
        return HttpResponse.json(marathon, { status: 201 })
      }),
    )
    renderApp('/races')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Add race' }))
    const dialog = within(await screen.findByRole('dialog', { name: 'Add race' }))

    await user.click(dialog.getByRole('button', { name: 'Save' }))
    expect(dialog.getByRole('alert')).toHaveTextContent('Enter a name.')
    await user.type(dialog.getByLabelText('Name'), 'City Marathon')
    await user.click(dialog.getByRole('button', { name: 'Save' }))
    expect(dialog.getByRole('alert')).toHaveTextContent('Enter the distance in km.')
    await user.type(dialog.getByLabelText('Distance (km)'), '42.195')
    await user.type(dialog.getByLabelText('Goal time (h:mm:ss, optional)'), 'soon')
    await user.click(dialog.getByRole('button', { name: 'Save' }))
    expect(dialog.getByRole('alert')).toHaveTextContent('Enter the goal time as h:mm:ss')

    await user.clear(dialog.getByLabelText('Goal time (h:mm:ss, optional)'))
    await user.type(dialog.getByLabelText('Goal time (h:mm:ss, optional)'), '3:15:00')
    await user.clear(dialog.getByLabelText('Day'))
    await user.type(dialog.getByLabelText('Day'), '2026-12-06')
    await user.click(dialog.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText(/No races yet/)).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(sent).toEqual([
      {
        name: 'City Marathon',
        day: '2026-12-06',
        sport: 'running',
        distanceM: 42195,
        goalTimeS: 11700,
        priority: 'A',
      },
    ])
  })

  it('edits a race from its current values, and deletes one', async () => {
    const sent: RaceInput[] = []
    let deleted = 0
    server.use(
      http.get('/api/races', () => HttpResponse.json({ items: [marathon] })),
      http.put('/api/races/1', async ({ request }) => {
        sent.push((await request.json()) as RaceInput)
        return HttpResponse.json(marathon)
      }),
      http.delete('/api/races/1', () => {
        deleted++
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderApp('/races')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Edit City Marathon' }))
    const dialog = within(await screen.findByRole('dialog', { name: 'Edit City Marathon' }))
    expect(dialog.getByLabelText('Distance (km)')).toHaveValue('42.195')
    expect(dialog.getByLabelText('Goal time (h:mm:ss, optional)')).toHaveValue('3:15:00')
    await user.clear(dialog.getByLabelText('Goal time (h:mm:ss, optional)'))
    await user.selectOptions(dialog.getByLabelText('Priority'), 'B')
    await user.click(dialog.getByRole('button', { name: 'Save' }))
    await vi.waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(sent).toEqual([
      {
        name: 'City Marathon',
        day: '2026-12-06',
        sport: 'running',
        distanceM: 42195,
        priority: 'B',
      },
    ])

    await user.click(screen.getByRole('button', { name: 'Delete City Marathon' }))
    await vi.waitFor(() => {
      expect(deleted).toBe(1)
    })
  })

  it('shows a failed save in the dialog', async () => {
    server.use(
      http.post('/api/races', () =>
        problem(400, 'invalid-request', 'Invalid request', 'unknown sport'),
      ),
    )
    renderApp('/races')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Add race' }))
    const dialog = within(await screen.findByRole('dialog', { name: 'Add race' }))
    await user.type(dialog.getByLabelText('Name'), 'Race')
    await user.type(dialog.getByLabelText('Distance (km)'), '10')
    await user.click(dialog.getByRole('button', { name: 'Save' }))
    expect(await dialog.findByRole('alert')).toHaveTextContent('unknown sport')
  })
})
