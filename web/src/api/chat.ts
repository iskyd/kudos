import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type ChatMessage = components['schemas']['ChatMessage']
type ChatEvent = components['schemas']['ChatEvent']

// Keep in step with ChatRequest in api/openapi.yaml.
export const maxChatMessages = 40

function streamFailure(detail: string): ApiError {
  return new ApiError({
    type: 'urn:kudos:problem:llm-stream',
    title: 'The AI coach failed',
    status: 502,
    detail,
  })
}

// streamChat sends the conversation and calls onText with each piece of the
// reply as it arrives. It resolves when the reply is complete and throws an
// ApiError if the request or the provider fails.
export async function streamChat(
  messages: ChatMessage[],
  csrfToken: string,
  onText: (text: string) => void,
  signal?: AbortSignal,
): Promise<void> {
  const { data, error } = await api.POST('/api/chat', {
    body: { messages },
    headers: { 'X-CSRF-Token': csrfToken },
    parseAs: 'stream',
    signal,
  })
  if (error) throw new ApiError(error)
  if (!data) throw streamFailure('The reply was empty.')
  const reader = data.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  for (;;) {
    const { value, done } = await reader.read()
    buffer += decoder.decode(value, { stream: !done })
    let end
    while ((end = buffer.indexOf('\n\n')) >= 0) {
      const line = buffer.slice(0, end)
      buffer = buffer.slice(end + 2)
      if (!line.startsWith('data: ')) continue
      const event = JSON.parse(line.slice(6)) as ChatEvent
      if (event.type === 'text') onText(event.text ?? '')
      else if (event.type === 'error') throw streamFailure(event.detail ?? 'Unknown error.')
      else return
    }
    if (done) throw streamFailure('The reply stopped before the end.')
  }
}
