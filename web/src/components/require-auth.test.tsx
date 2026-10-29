import { screen } from '@testing-library/react'
import { loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'
import { http } from 'msw'

describe('RequireAuth', () => {
  it('sends visitors to login', async () => {
    loggedInAs(null)
    const router = renderApp('/activities')
    expect(await screen.findByRole('heading', { name: 'Log in' })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/login')
  })

  it('shows loading, then an error when the user cannot be loaded', async () => {
    server.use(http.get('/api/me', () => problem(500, 'internal', 'Internal error')))
    renderApp('/')
    expect(screen.getByText('Loading…')).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('Internal error')
  })
})
