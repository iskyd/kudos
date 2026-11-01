import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import type { Records } from '@/api/analytics'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

describe('RecordsPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it('shows loading, then a failure', async () => {
    server.use(http.get('/api/records', () => delay('infinite')))
    renderApp('/records')
    expect(await screen.findByText('Loading…')).toBeInTheDocument()

    server.use(http.get('/api/records', () => problem(500, 'internal', 'Something went wrong')))
    renderApp('/records')
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong')
  })

  it('shows an empty state without records', async () => {
    renderApp('/records')
    expect(await screen.findByText(/No records yet/)).toBeInTheDocument()
  })

  it('shows each record with when it was set and its history', async () => {
    const records: Records = {
      since: '2025-11-19',
      records: [
        {
          effort: 'fastest_5k',
          history: [
            { activityId: 3, startTime: '2026-10-15T16:40:13Z', utcOffsetS: 7200, value: 1400 },
            { activityId: 5, startTime: '2026-10-25T08:35:15Z', utcOffsetS: 3600, value: 1320.8 },
          ],
        },
        {
          effort: 'power_20m',
          history: [
            { activityId: 6, startTime: '2026-10-20T17:51:37Z', utcOffsetS: 7200, value: 133.5 },
          ],
        },
      ],
    }
    server.use(http.get('/api/records', () => HttpResponse.json(records)))
    renderApp('/records')

    expect(await screen.findByText(/Personal bests since 19 November 2025/)).toBeInTheDocument()
    const rows = within(screen.getByRole('table', { name: 'Records' })).getAllByRole('row')
    expect(rows[1]).toHaveTextContent(/Fastest 5K\s*22:01\s*25 Oct 2026, 09:35/)
    expect(within(rows[1] as HTMLElement).getAllByRole('link')[0]).toHaveAttribute(
      'href',
      '/activities/5',
    )
    expect(rows[2]).toHaveTextContent(/Best 20 min power\s*134 W\s*20 Oct 2026, 19:51/)
    expect(within(rows[2] as HTMLElement).queryByText(/Set \d+ times/)).not.toBeInTheDocument()

    // The history lists every time the record was set, newest first.
    await userEvent.click(within(rows[1] as HTMLElement).getByText('Set 2 times'))
    const history = within(rows[1] as HTMLElement).getAllByRole('listitem')
    expect(history.map((li) => li.textContent)).toEqual([
      '22:01 on 25 Oct 2026, 09:35',
      '23:20 on 15 Oct 2026, 18:40',
    ])
  })
})
