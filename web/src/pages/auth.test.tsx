import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

async function fill(username: string, password: string) {
  if (username) await userEvent.type(await screen.findByLabelText('Username'), username)
  if (password) await userEvent.type(screen.getByLabelText('Password'), password)
}

describe('LoginPage', () => {
  beforeEach(() => {
    loggedInAs(null)
  })

  it('validates required fields without calling the API', async () => {
    const onLogin = vi.fn()
    server.use(http.post('/api/auth/login', onLogin))
    renderApp('/login')
    await userEvent.click(await screen.findByRole('button', { name: 'Log in' }))
    expect(screen.getByLabelText('Username')).toHaveAccessibleDescription('Enter a username.')
    expect(screen.getByLabelText('Password')).toHaveAccessibleDescription('Enter a password.')
    expect(onLogin).not.toHaveBeenCalled()
  })

  it('shows invalid credentials', async () => {
    server.use(
      http.post('/api/auth/login', () =>
        problem(401, 'invalid-credentials', 'Invalid username or password'),
      ),
    )
    renderApp('/login')
    await fill('ann', 'wrong-password')
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Invalid username or password')
  })

  it('shows network errors', async () => {
    server.use(http.post('/api/auth/login', () => HttpResponse.error()))
    renderApp('/login')
    await fill('ann', 'password1')
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not reach the server')
  })

  it('logs in and returns to the guarded page', async () => {
    let body: unknown
    server.use(
      http.post('/api/auth/login', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json(ann)
      }),
    )
    const router = renderApp('/plans')
    await fill('ann', 'password1')
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }))
    expect(await screen.findByRole('heading', { name: 'Plans' })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/plans')
    expect(body).toEqual({ username: 'ann', password: 'password1' })
  })

  it('redirects a logged-in user to the dashboard', async () => {
    loggedInAs(ann)
    renderApp('/login')
    expect(await screen.findByRole('heading', { name: 'Dashboard' })).toBeInTheDocument()
  })

  it('links to registration', async () => {
    renderApp('/login')
    await userEvent.click(await screen.findByRole('link', { name: 'Register' }))
    expect(screen.getByRole('heading', { name: 'Create an account' })).toBeInTheDocument()
  })
})

describe('RegisterPage', () => {
  beforeEach(() => {
    loggedInAs(null)
  })

  it('validates username and password rules', async () => {
    const onRegister = vi.fn()
    server.use(http.post('/api/auth/register', onRegister))
    renderApp('/register')
    await fill('a b', 'short')
    await userEvent.click(screen.getByRole('button', { name: 'Register' }))
    expect(screen.getByLabelText('Username')).toHaveAccessibleDescription(
      'Use 3 to 32 letters, digits, ".", "_" or "-".',
    )
    expect(screen.getByLabelText('Password')).toHaveAccessibleDescription(
      'Use 8 to 256 characters.',
    )
    expect(screen.getByLabelText('Username')).toHaveAttribute('aria-invalid', 'true')
    expect(onRegister).not.toHaveBeenCalled()
  })

  it.each([
    { kind: 'username-taken', status: 409, title: 'Username already taken' },
    { kind: 'registration-disabled', status: 403, title: 'Registration is disabled' },
  ])('shows $kind', async ({ kind, status, title }) => {
    server.use(http.post('/api/auth/register', () => problem(status, kind, title)))
    renderApp('/register')
    await fill('ann', 'password1')
    await userEvent.click(screen.getByRole('button', { name: 'Register' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(title)
  })

  it('registers with the browser timezone and opens the app', async () => {
    let body: unknown
    server.use(
      http.post('/api/auth/register', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json(ann, { status: 201 })
      }),
    )
    renderApp('/register')
    await fill('ann', 'password1')
    await userEvent.click(screen.getByRole('button', { name: 'Register' }))
    expect(await screen.findByRole('heading', { name: 'Dashboard' })).toBeInTheDocument()
    expect(body).toEqual({
      username: 'ann',
      password: 'password1',
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    })
  })
})
