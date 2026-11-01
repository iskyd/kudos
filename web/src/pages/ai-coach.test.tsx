import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import type { LLMProvider, LLMProviderInput } from '@/api/llm'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

const saved: LLMProvider = {
  configured: true,
  provider: 'anthropic',
  baseUrl: 'https://api.anthropic.com',
  model: 'claude-test',
  keySet: true,
}

// serve answers the provider endpoints from one stored value, recording what was sent.
function serve(initial: LLMProvider) {
  let current = initial
  const sent: LLMProviderInput[] = []
  let deleted = 0
  server.use(
    http.get('/api/llm-provider', () => HttpResponse.json(current)),
    http.put('/api/llm-provider', async ({ request }) => {
      const body = (await request.json()) as LLMProviderInput
      sent.push(body)
      current = {
        configured: true,
        provider: body.provider,
        baseUrl: body.baseUrl ?? 'https://default.example.test',
        model: body.model,
        keySet: body.apiKey !== undefined || (current.keySet && current.provider === body.provider),
      }
      return HttpResponse.json(current)
    }),
    http.delete('/api/llm-provider', () => {
      deleted++
      current = { configured: false, keySet: false }
      return new HttpResponse(null, { status: 204 })
    }),
  )
  return { sent, deleted: () => deleted }
}

describe('AICoachPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it('is linked from the settings', async () => {
    renderApp('/settings')
    expect(await screen.findByRole('link', { name: 'Set up the AI coach' })).toHaveAttribute(
      'href',
      '/settings/ai-coach',
    )
  })

  it('shows loading, then a failure', async () => {
    server.use(http.get('/api/llm-provider', () => delay('infinite')))
    renderApp('/settings/ai-coach')
    expect(await screen.findByText('Loading…')).toBeInTheDocument()

    server.use(
      http.get('/api/llm-provider', () => problem(500, 'internal', 'Something went wrong')),
    )
    renderApp('/settings/ai-coach')
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong')
  })

  it('says plainly what cloud providers receive', async () => {
    serve({ configured: false, keySet: false })
    renderApp('/settings/ai-coach')
    const notice = await screen.findByRole('region', { name: 'What the provider receives' })
    expect(notice).toHaveTextContent(/full injury and illness history/)
    expect(notice).toHaveTextContent(/values derived from GPS/)
    expect(notice).toHaveTextContent(/leaves your server/)
    expect(notice).toHaveTextContent(/With Ollama it stays on a machine you run/)
  })

  it('sets up a provider, keeping the key write-only', async () => {
    const api = serve({ configured: false, keySet: false })
    renderApp('/settings/ai-coach')
    await userEvent.selectOptions(await screen.findByLabelText('Provider'), 'anthropic')
    expect(screen.queryByRole('button', { name: 'Remove provider' })).not.toBeInTheDocument()
    await userEvent.type(screen.getByLabelText('Model'), 'claude-test')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Enter the API key.')
    expect(api.sent).toHaveLength(0)

    const key = screen.getByLabelText('API key')
    expect(key).toHaveAttribute('type', 'password')
    await userEvent.type(key, 'sk-secret-key')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Saved.')
    expect(api.sent).toEqual([
      { provider: 'anthropic', model: 'claude-test', apiKey: 'sk-secret-key' },
    ])
    // The typed key is cleared and never shown again.
    expect(screen.getByLabelText('API key')).toHaveValue('')
    expect(screen.getByLabelText('API key')).toHaveAttribute(
      'placeholder',
      'Saved. Leave empty to keep it.',
    )
    expect(screen.queryByDisplayValue('sk-secret-key')).not.toBeInTheDocument()
  })

  it('keeps the stored key for the same provider and asks for one when switching', async () => {
    const api = serve(saved)
    renderApp('/settings/ai-coach')
    const model = await screen.findByLabelText('Model')
    expect(model).toHaveValue('claude-test')
    expect(screen.getByLabelText('Base URL (optional)')).toHaveValue('https://api.anthropic.com')
    await userEvent.clear(model)
    await userEvent.type(model, 'claude-next')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Saved.')
    expect(api.sent[0]).toEqual({
      provider: 'anthropic',
      model: 'claude-next',
      baseUrl: 'https://api.anthropic.com',
    })

    await userEvent.selectOptions(screen.getByLabelText('Provider'), 'openai')
    expect(screen.getByLabelText('API key')).toHaveAttribute('placeholder', '')
    expect(screen.getByLabelText('Base URL (optional)')).toHaveValue('')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Enter the API key.')
    expect(api.sent).toHaveLength(1)
  })

  it('needs no key for Ollama and shows a refused save', async () => {
    const api = serve({ configured: false, keySet: false })
    renderApp('/settings/ai-coach')
    expect(await screen.findByLabelText('API key (optional)')).toBeInTheDocument()
    await userEvent.type(
      screen.getByLabelText('Base URL (optional)'),
      'http://ollama.example.test:11434/v1',
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Enter a model name.')
    await userEvent.type(screen.getByLabelText('Model'), 'llama3')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Saved.')
    expect(api.sent).toEqual([
      { provider: 'ollama', model: 'llama3', baseUrl: 'http://ollama.example.test:11434/v1' },
    ])

    server.use(
      http.put('/api/llm-provider', () =>
        problem(400, 'invalid_request', 'baseUrl must be an http or https URL'),
      ),
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'baseUrl must be an http or https URL',
    )
  })

  it('removes the provider', async () => {
    const api = serve(saved)
    renderApp('/settings/ai-coach')
    await userEvent.click(await screen.findByRole('button', { name: 'Remove provider' }))
    expect(await screen.findByLabelText('Model')).toHaveValue('')
    expect(screen.queryByRole('button', { name: 'Remove provider' })).not.toBeInTheDocument()
    expect(api.deleted()).toBe(1)
  })
})
