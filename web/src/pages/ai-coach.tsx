import { useState, type SubmitEvent } from 'react'
import { Link } from 'react-router'
import {
  type LLMProvider,
  type LLMProviderKind,
  useDeleteLLMProvider,
  useLLMProvider,
  useSetLLMProvider,
} from '@/api/llm'
import { problemMessage } from '@/api/problem'
import { QueryStatus } from '@/components/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

const providerLabels: Record<LLMProviderKind, string> = {
  ollama: 'Ollama (runs on your own machine)',
  openai: 'OpenAI',
  gemini: 'Google Gemini',
  perplexity: 'Perplexity',
  anthropic: 'Anthropic',
}

export function AICoachPage() {
  const provider = useLLMProvider()
  // Here, not in the form: saving can remount the form (its key is the provider).
  const save = useSetLLMProvider()
  const remove = useDeleteLLMProvider()
  return (
    <div className="flex max-w-xl flex-col gap-6">
      <div className="flex flex-col gap-1">
        <Link to="/settings" className="text-sm underline">
          Settings
        </Link>
        <h1 className="text-2xl font-semibold">AI coach</h1>
      </div>
      <section aria-labelledby="privacy-heading" className="flex flex-col gap-2 text-sm">
        <h2 id="privacy-heading" className="text-lg font-semibold">
          What the provider receives
        </h2>
        <p>
          The coach sends your provider a summary of your data with every request: your profile and
          zones, your full injury and illness history, upcoming races and goals, readiness and
          check-ins, training load, and recent activity summaries with values derived from GPS such
          as distance, pace and elevation.
        </p>
        <p>
          With OpenAI, Google Gemini, Perplexity or Anthropic, this health and training data leaves
          your server and is handled under that company&apos;s terms. With Ollama it stays on a
          machine you run. KudOS computes every number itself; the model only explains them.
        </p>
      </section>
      <section aria-labelledby="provider-heading" className="flex flex-col gap-4">
        <h2 id="provider-heading" className="text-lg font-semibold">
          Connection
        </h2>
        <QueryStatus query={provider} />
        {provider.data && (
          <ProviderForm
            key={provider.data.provider}
            saved={provider.data}
            save={save}
            remove={remove}
          />
        )}
      </section>
    </div>
  )
}

function ProviderForm({
  saved,
  save,
  remove,
}: {
  saved: LLMProvider
  save: ReturnType<typeof useSetLLMProvider>
  remove: ReturnType<typeof useDeleteLLMProvider>
}) {
  const [kind, setKind] = useState<LLMProviderKind>(saved.provider ?? 'ollama')
  const [invalid, setInvalid] = useState<string | null>(null)
  // A stored key belongs to the saved provider; switching needs a new one.
  const keyStored = saved.keySet && kind === saved.provider

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    const formElement = event.currentTarget
    const form = new FormData(formElement)
    const field = (name: string) => {
      const value = form.get(name)
      return typeof value === 'string' ? value.trim() : ''
    }
    const model = field('model')
    const apiKey = field('api-key')
    if (model === '') {
      setInvalid('Enter a model name.')
      return
    }
    if (kind !== 'ollama' && apiKey === '' && !keyStored) {
      setInvalid('Enter the API key.')
      return
    }
    setInvalid(null)
    const baseUrl = field('base-url')
    save.mutate(
      {
        provider: kind,
        model,
        ...(baseUrl !== '' && { baseUrl }),
        ...(apiKey !== '' && { apiKey }),
      },
      {
        // The key field never keeps what was typed.
        onSuccess: () => {
          const key = formElement.elements.namedItem('api-key')
          if (key instanceof HTMLInputElement) key.value = ''
        },
      },
    )
  }

  return (
    <form noValidate onSubmit={onSubmit} className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <Label htmlFor="llm-provider">Provider</Label>
        <NativeSelect
          id="llm-provider"
          value={kind}
          onChange={(event) => {
            setKind(event.target.value as LLMProviderKind)
            save.reset()
          }}
        >
          {Object.entries(providerLabels).map(([value, label]) => (
            <NativeSelectOption key={value} value={value}>
              {label}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="llm-base-url">Base URL (optional)</Label>
        <Input
          key={kind}
          id="llm-base-url"
          name="base-url"
          inputMode="url"
          defaultValue={kind === saved.provider ? saved.baseUrl : ''}
          placeholder={
            kind === 'ollama' ? 'http://localhost:11434/v1' : "Empty for the provider's public API"
          }
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="llm-model">Model</Label>
        <Input id="llm-model" name="model" defaultValue={saved.model} />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="llm-api-key">API key{kind === 'ollama' ? ' (optional)' : ''}</Label>
        <Input
          id="llm-api-key"
          name="api-key"
          type="password"
          autoComplete="off"
          placeholder={keyStored ? 'Saved. Leave empty to keep it.' : ''}
        />
        <p className="text-xs text-muted-foreground">Stored encrypted and never shown again.</p>
      </div>
      {(invalid ?? save.isError) && (
        <p role="alert" className="text-sm text-destructive">
          {invalid ?? problemMessage(save.error)}
        </p>
      )}
      {save.isSuccess && (
        <p role="status" className="text-sm text-muted-foreground">
          Saved.
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" disabled={save.isPending}>
          Save
        </Button>
        {saved.configured && (
          <Button
            type="button"
            variant="outline"
            disabled={remove.isPending}
            onClick={() => {
              remove.mutate()
            }}
          >
            Remove provider
          </Button>
        )}
      </div>
      {remove.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(remove.error)}
        </p>
      )}
    </form>
  )
}
