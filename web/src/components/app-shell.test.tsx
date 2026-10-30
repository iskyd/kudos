import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { navItems } from '@/components/nav-items'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

describe('AppShell', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it('links every section', async () => {
    renderApp('/')
    const nav = await screen.findByRole('navigation', { name: 'Main' })
    for (const { label } of navItems) {
      expect(nav).toContainElement(screen.getByRole('link', { name: label }))
    }
  })

  it.each(navItems)('renders $label at $path', async ({ path, label }) => {
    renderApp(path)
    expect(await screen.findByRole('heading', { name: label })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: label })).toHaveAttribute('aria-current', 'page')
  })

  it('navigates on click', async () => {
    renderApp('/')
    await userEvent.click(await screen.findByRole('link', { name: 'Plans' }))
    expect(screen.getByRole('heading', { name: 'Plans' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Dashboard' })).not.toHaveAttribute('aria-current')
  })

  it('renders not found for unknown paths', async () => {
    renderApp('/nope')
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
  })

  it.each([
    { name: 'logs out', status: 204 },
    { name: 'treats an expired session as logged out', status: 401 },
  ])('$name', async ({ status }) => {
    let csrf: string | null = null
    server.use(
      http.post('/api/auth/logout', ({ request }) => {
        csrf = request.headers.get('X-CSRF-Token')
        return status === 204
          ? new HttpResponse(null, { status })
          : problem(status, 'unauthorized', 'Not logged in')
      }),
    )
    const router = renderApp('/')
    await userEvent.click(await screen.findByRole('button', { name: 'Log out' }))
    expect(await screen.findByRole('heading', { name: 'Log in' })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/login')
    expect(csrf).toBe('csrf-ann')
  })

  it('shows logout errors', async () => {
    server.use(
      http.post('/api/auth/logout', () => problem(403, 'csrf', 'Missing or invalid CSRF token')),
    )
    renderApp('/')
    await userEvent.click(await screen.findByRole('button', { name: 'Log out' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Missing or invalid CSRF token')
  })

  it('shows the Garmin banner on every page with a link to Settings', async () => {
    server.use(
      // Answered after the page's own queries, so no other alert may show first.
      http.get('/api/garmin', async () => {
        await new Promise((r) => setTimeout(r, 50))
        return HttpResponse.json({
          connected: true,
          status: 'key_rejected',
          keyExpiresOn: '2099-01-01',
        })
      }),
      http.get('/api/garmin/backfill', () => HttpResponse.json({ started: false })),
      http.get('/api/activities', () => HttpResponse.json({ items: [] })),
    )
    renderApp('/activities')
    expect(await screen.findByRole('alert')).toHaveTextContent('Garmin rejected the key.')
    await userEvent.click(screen.getByRole('link', { name: 'Open Settings' }))
    expect(await screen.findByRole('heading', { name: 'Settings' })).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Garmin rejected the key.')
    expect(screen.queryByRole('link', { name: 'Open Settings' })).not.toBeInTheDocument()
  })

  it('shows a failed sync as a warning', async () => {
    server.use(
      http.get('/api/garmin', () =>
        HttpResponse.json({
          connected: true,
          status: 'connected',
          keyExpiresOn: '2099-01-01',
          lastError: 'Garmin failed: status 503',
        }),
      ),
    )
    renderApp('/')
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The last Garmin sync failed: Garmin failed: status 503',
    )
  })

  it('shows no banner when Garmin is fine or unreachable', async () => {
    server.use(http.get('/api/garmin', () => problem(500, 'internal', 'Internal error')))
    renderApp('/')
    expect(await screen.findByRole('heading', { name: 'Dashboard' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
