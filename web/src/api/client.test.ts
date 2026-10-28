import { http, HttpResponse } from 'msw'
import { api } from '@/api/client'
import { server } from '@/test/server'

describe('api client', () => {
  it('calls the same-origin API', async () => {
    server.use(http.get('/healthz', () => HttpResponse.text('ok')))
    const { data, response } = await api.GET('/healthz', { parseAs: 'text' })
    expect(response.status).toBe(200)
    expect(data).toBe('ok')
  })

  it('returns problem details on errors', async () => {
    const problem = { type: 'urn:kudos:problem:internal', title: 'Internal error', status: 500 }
    server.use(
      http.get('/healthz', () =>
        HttpResponse.json(problem, {
          status: 500,
          headers: { 'Content-Type': 'application/problem+json' },
        }),
      ),
    )
    const { error } = await api.GET('/healthz')
    expect(error).toEqual(problem)
  })
})
