import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import type { CoachReport } from '@/api/reports'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const report = (id: number, day: string, content: string): CoachReport => ({
  id,
  kind: 'weekly_report',
  day,
  promptVersion: 'weekly-report.v1',
  content,
  createdAt: `${day}T06:30:00Z`,
})

describe('ReportsPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it('is in the navigation', async () => {
    renderApp('/')
    expect(await screen.findByRole('link', { name: 'Reports' })).toHaveAttribute('href', '/reports')
  })

  it('shows loading, then a failure', async () => {
    server.use(http.get('/api/coach-reports', () => delay('infinite')))
    renderApp('/reports')
    expect(await screen.findByText('Loading…')).toBeInTheDocument()

    server.use(
      http.get('/api/coach-reports', () => problem(500, 'internal', 'Something went wrong')),
    )
    renderApp('/reports')
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong')
  })

  it('says how to get reports when there are none', async () => {
    renderApp('/reports')
    expect(await screen.findByText(/No weekly reports yet/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Set up the AI coach' })).toHaveAttribute(
      'href',
      '/settings/ai-coach',
    )
  })

  it('lists reports by the week they cover, page by page', async () => {
    server.use(
      http.get('/api/llm-provider', () =>
        HttpResponse.json({
          configured: true,
          provider: 'ollama',
          baseUrl: 'http://o.example.test',
          model: 'm',
          keySet: false,
        }),
      ),
      http.get('/api/coach-reports', ({ request }) => {
        const query = new URL(request.url).searchParams
        expect(query.get('kind')).toBe('weekly_report')
        if (query.get('cursor') === 'next') {
          return HttpResponse.json({ items: [report(1, '2026-11-02', 'Older week.')] })
        }
        return HttpResponse.json({
          items: [report(2, '2026-11-09', '## Volume\nLong run [act:12] on [day:2026-11-08].')],
          nextCursor: 'next',
        })
      }),
    )
    renderApp('/reports')
    const latest = await screen.findByRole('article', { name: 'Week of 2 November 2026' })
    expect(latest).toHaveTextContent('## Volume')
    expect(screen.getByRole('link', { name: 'activity 12' })).toHaveAttribute(
      'href',
      '/activities/12',
    )
    expect(screen.getByRole('link', { name: '8 November 2026' })).toHaveAttribute(
      'href',
      '/calendar?day=2026-11-08',
    )
    expect(screen.queryByText(/No weekly reports yet/)).not.toBeInTheDocument()

    await userEvent.setup().click(screen.getByRole('button', { name: 'Show more' }))
    expect(
      await screen.findByRole('article', { name: 'Week of 26 October 2026' }),
    ).toHaveTextContent('Older week.')
    expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument()
  })
})
