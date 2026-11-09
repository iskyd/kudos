import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { delay, http, HttpResponse } from 'msw'
import type { components } from '@/api/generated/schema'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

type ChatEvent = components['schemas']['ChatEvent']
type ChatRequest = components['schemas']['ChatRequest']

const configured = {
  configured: true,
  provider: 'ollama',
  baseUrl: 'http://ollama.example.test',
  model: 'm',
  keySet: false,
}

function sse(...events: ChatEvent[]) {
  return events.map((e) => `data: ${JSON.stringify(e)}\n\n`).join('')
}

const streamHeaders = { headers: { 'Content-Type': 'text/event-stream' } }

// answer replies to every chat request with body, recording what was sent.
function answer(body: () => BodyInit) {
  const sent: ChatRequest[] = []
  server.use(
    http.post('/api/chat', async ({ request }) => {
      sent.push((await request.json()) as ChatRequest)
      return new HttpResponse(body(), streamHeaders)
    }),
  )
  return sent
}

async function ask(question: string) {
  const user = userEvent.setup()
  await user.type(await screen.findByLabelText('Your question'), question)
  await user.click(screen.getByRole('button', { name: 'Ask' }))
}

describe('CoachPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
    server.use(http.get('/api/llm-provider', () => HttpResponse.json(configured)))
  })

  it('is in the navigation', async () => {
    renderApp('/')
    expect(await screen.findByRole('link', { name: 'Coach' })).toHaveAttribute('href', '/coach')
  })

  it('shows loading, then a failure', async () => {
    server.use(http.get('/api/llm-provider', () => delay('infinite')))
    renderApp('/coach')
    expect(await screen.findByText('Loading…')).toBeInTheDocument()

    server.use(
      http.get('/api/llm-provider', () => problem(500, 'internal', 'Something went wrong')),
    )
    renderApp('/coach')
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong')
  })

  it('sends a user without a provider to set one up', async () => {
    server.use(
      http.get('/api/llm-provider', () => HttpResponse.json({ configured: false, keySet: false })),
    )
    renderApp('/coach')
    expect(await screen.findByRole('link', { name: 'Set up the AI coach' })).toHaveAttribute(
      'href',
      '/settings/ai-coach',
    )
    expect(screen.queryByLabelText('Your question')).not.toBeInTheDocument()
  })

  it('streams the reply and links its references', async () => {
    const encoder = new TextEncoder()
    let stream!: ReadableStreamDefaultController<Uint8Array>
    const sent = answer(
      () =>
        new ReadableStream<Uint8Array>({
          start(c) {
            stream = c
          },
        }),
    )
    renderApp('/coach')
    await ask('How was my week?')

    const conversation = await screen.findByRole('list', { name: 'Conversation' })
    expect(within(conversation).getByText('How was my week?')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Answering…' })).toBeDisabled()

    stream.enqueue(encoder.encode(sse({ type: 'text', text: 'Easy run ' })))
    expect(await within(conversation).findByText('Easy run')).toBeInTheDocument()
    // An event can arrive split across chunks.
    const rest = sse({ type: 'text', text: '[act:12] on [day:2026-11-02].' }, { type: 'done' })
    stream.enqueue(encoder.encode(rest.slice(0, 10)))
    stream.enqueue(encoder.encode(rest.slice(10)))
    stream.close()

    expect(await screen.findByRole('link', { name: 'activity 12' })).toHaveAttribute(
      'href',
      '/activities/12',
    )
    expect(screen.getByRole('link', { name: '2 November 2026' })).toHaveAttribute(
      'href',
      '/calendar?day=2026-11-02',
    )
    expect(await screen.findByRole('button', { name: 'Ask' })).toBeInTheDocument()
    expect(sent).toEqual([{ messages: [{ role: 'user', content: 'How was my week?' }] }])
  })

  it('sends the whole conversation each turn, and starts over', async () => {
    const sent = answer(() => sse({ type: 'text', text: 'Rest.' }, { type: 'done' }))
    renderApp('/coach')
    await ask('Tired?')
    expect(await screen.findByText('Rest.')).toBeInTheDocument()
    await ask('Tomorrow?')
    expect(await screen.findAllByText('Rest.')).toHaveLength(2)
    expect(sent[1]).toEqual({
      messages: [
        { role: 'user', content: 'Tired?' },
        { role: 'assistant', content: 'Rest.' },
        { role: 'user', content: 'Tomorrow?' },
      ],
    })

    await userEvent.setup().click(screen.getByRole('button', { name: 'New conversation' }))
    expect(screen.queryByRole('list', { name: 'Conversation' })).not.toBeInTheDocument()
    await ask('Hi')
    await screen.findByText('Rest.')
    expect(sent[2]).toEqual({ messages: [{ role: 'user', content: 'Hi' }] })
  })

  it.each([
    [
      'a problem before the stream',
      () => problem(409, 'llm-not-configured', 'No AI coach provider set up', 'Set one up first'),
      'Set one up first',
    ],
    [
      'a provider error in the stream',
      () =>
        new HttpResponse(
          sse(
            { type: 'text', text: 'Par' },
            { type: 'error', detail: 'LLM provider error (401): bad key' },
          ),
          streamHeaders,
        ),
      'LLM provider error (401): bad key',
    ],
    [
      'a stream cut short',
      () => new HttpResponse(sse({ type: 'text', text: 'Par' }), streamHeaders),
      'The reply stopped before the end.',
    ],
  ])('shows %s and gives the question back', async (_, reply, message) => {
    server.use(http.post('/api/chat', reply))
    renderApp('/coach')
    await ask('Why?')
    expect(await screen.findByRole('alert')).toHaveTextContent(message)
    expect(screen.getByLabelText('Your question')).toHaveValue('Why?')
    expect(screen.queryByRole('list', { name: 'Conversation' })).not.toBeInTheDocument()
  })
})
