import { useEffect, useRef, useState, type SubmitEvent } from 'react'
import { Link } from 'react-router'
import { useCurrentUser } from '@/api/auth'
import { type ChatMessage, maxChatMessages, streamChat } from '@/api/chat'
import { useLLMProvider } from '@/api/llm'
import { problemMessage } from '@/api/problem'
import { CoachText } from '@/components/coach-text'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

function Turn({ message }: { message: ChatMessage }) {
  const user = message.role === 'user'
  return (
    <li className={user ? 'self-end rounded-md bg-muted px-3 py-2' : 'self-start'}>
      <span className="sr-only">{user ? 'You:' : 'Coach:'}</span>
      {user ? (
        <p className="whitespace-pre-wrap">{message.content}</p>
      ) : (
        <CoachText text={message.content} />
      )}
    </li>
  )
}

export function CoachPage() {
  const provider = useLLMProvider()
  return (
    <div className="flex max-w-2xl flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold">Coach</h1>
        <p className="text-sm text-muted-foreground">
          Answers come from your data and link to what they cite. Conversations are not saved.
        </p>
      </div>
      {provider.isPending ? (
        <p>Loading…</p>
      ) : provider.isError ? (
        <p role="alert" className="text-destructive">
          {problemMessage(provider.error)}
        </p>
      ) : !provider.data.configured ? (
        <p>
          No AI coach is set up yet.{' '}
          <Link to="/settings/ai-coach" className="underline">
            Set up the AI coach
          </Link>
        </p>
      ) : (
        <Chat />
      )}
    </div>
  )
}

function Chat() {
  const { csrfToken } = useCurrentUser()
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [question, setQuestion] = useState('')
  const [pending, setPending] = useState<{ question: string; reply: string } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const abort = useRef<AbortController | null>(null)
  useEffect(() => () => abort.current?.abort(), [])
  // The next question and its reply must fit in one request.
  const full = messages.length + 2 > maxChatMessages

  async function onSubmit(e: SubmitEvent<HTMLFormElement>) {
    e.preventDefault()
    const asked = question.trim()
    if (!asked || pending) return
    const history: ChatMessage[] = [...messages, { role: 'user', content: asked }]
    const controller = new AbortController()
    abort.current = controller
    let reply = ''
    setQuestion('')
    setError(null)
    setPending({ question: asked, reply })
    try {
      await streamChat(
        history,
        csrfToken,
        (text) => {
          reply += text
          setPending({ question: asked, reply })
        },
        controller.signal,
      )
      setMessages([...history, { role: 'assistant', content: reply }])
    } catch (err) {
      if (controller.signal.aborted) return
      // The failed turn leaves the conversation; the question goes back in the box.
      setError(problemMessage(err))
      setQuestion(asked)
    }
    setPending(null)
  }

  return (
    <div className="flex flex-col gap-4">
      {messages.length > 0 || pending ? (
        <ol aria-label="Conversation" className="flex flex-col gap-3">
          {messages.map((m, i) => (
            <Turn key={i} message={m} />
          ))}
          {pending && (
            <>
              <Turn message={{ role: 'user', content: pending.question }} />
              <Turn message={{ role: 'assistant', content: pending.reply || '…' }} />
            </>
          )}
        </ol>
      ) : (
        <p className="text-muted-foreground">
          Ask about your training, recovery or what to do today.
        </p>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {full ? (
        <p role="status" className="text-sm text-muted-foreground">
          This conversation is as long as it can get. Start a new one to go on.
        </p>
      ) : (
        <form
          noValidate
          onSubmit={(e) => {
            void onSubmit(e)
          }}
          className="flex flex-col gap-2"
        >
          <Label htmlFor="coach-question">Your question</Label>
          <Textarea
            id="coach-question"
            value={question}
            maxLength={8000}
            onChange={(e) => {
              setQuestion(e.target.value)
            }}
          />
          <Button
            type="submit"
            className="self-start"
            disabled={pending !== null || !question.trim()}
          >
            {pending ? 'Answering…' : 'Ask'}
          </Button>
        </form>
      )}
      {messages.length > 0 && (
        <Button
          type="button"
          variant="outline"
          className="self-start"
          disabled={pending !== null}
          onClick={() => {
            setMessages([])
            setError(null)
          }}
        >
          New conversation
        </Button>
      )}
    </div>
  )
}
