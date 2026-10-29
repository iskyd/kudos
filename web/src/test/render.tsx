import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import type { Me } from '@/api/auth'
import type { Problem } from '@/api/problem'
import { routes } from '@/routes'
import { server } from '@/test/server'

export const ann: Me = { id: 1, username: 'ann', timezone: 'Europe/Rome', csrfToken: 'csrf-ann' }

// renderApp renders the full route tree at path with a fresh query cache.
export function renderApp(path: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return router
}

export function problem(status: number, kind: string, title: string, detail?: string) {
  const body: Problem = { type: `urn:kudos:problem:${kind}`, title, status, detail }
  return HttpResponse.json(body, {
    status,
    headers: { 'Content-Type': 'application/problem+json' },
  })
}

// loggedInAs answers GET /api/me with user, or with 401 when user is null. The
// app shell's Garmin banner asks for the connection on every page: none by default.
export function loggedInAs(user: Me | null) {
  server.use(
    http.get('/api/me', () =>
      user ? HttpResponse.json(user) : problem(401, 'unauthorized', 'Not logged in'),
    ),
    http.get('/api/garmin', () => HttpResponse.json({ connected: false })),
  )
}
