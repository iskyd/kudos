import { useState, type ComponentProps, type SubmitEvent } from 'react'
import { Link, Navigate, useLocation } from 'react-router'
import { useLogin, useMe, useRegister } from '@/api/auth'
import { problemMessage } from '@/api/problem'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { validateCredentials, type CredentialErrors } from '@/lib/credentials'

export function LoginPage() {
  return <CredentialsPage mode="login" />
}

export function RegisterPage() {
  return <CredentialsPage mode="register" />
}

const copy = {
  login: {
    title: 'Log in',
    submit: 'Log in',
    passwordAutocomplete: 'current-password',
    switchText: 'No account yet?',
    switchLink: { to: '/register', label: 'Register' },
  },
  register: {
    title: 'Create an account',
    submit: 'Register',
    passwordAutocomplete: 'new-password',
    switchText: 'Already registered?',
    switchLink: { to: '/login', label: 'Log in' },
  },
} as const

function CredentialsPage({ mode }: { mode: 'login' | 'register' }) {
  const me = useMe()
  const location = useLocation()
  const login = useLogin()
  const register = useRegister()
  const mutation = mode === 'login' ? login : register
  const [errors, setErrors] = useState<CredentialErrors>({})
  const text = copy[mode]

  // Logged in (already, or by this form): go where the guard sent us from.
  if (me.data) {
    const from = (location.state as { from?: string } | null)?.from ?? '/'
    return <Navigate to={from} replace />
  }

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const field = (name: string) => {
      const value = form.get(name)
      return typeof value === 'string' ? value : ''
    }
    const username = field('username')
    const password = field('password')
    const found = validateCredentials(username, password, mode)
    setErrors(found)
    if (found.username || found.password) return
    mutation.mutate({
      username,
      password,
      // The browser's zone is the best first guess; it can be changed in settings.
      ...(mode === 'register' && { timezone: Intl.DateTimeFormat().resolvedOptions().timeZone }),
    })
  }

  return (
    <main className="flex min-h-svh items-center justify-center bg-background p-6 text-foreground">
      <form noValidate onSubmit={onSubmit} className="flex w-full max-w-sm flex-col gap-4">
        <span className="text-lg font-semibold">KudOS</span>
        <h1 className="text-2xl font-semibold">{text.title}</h1>
        <Field name="username" label="Username" autoComplete="username" error={errors.username} />
        <Field
          name="password"
          label="Password"
          type="password"
          autoComplete={text.passwordAutocomplete}
          error={errors.password}
        />
        {mutation.isError && (
          <p role="alert" className="text-sm text-destructive">
            {problemMessage(mutation.error)}
          </p>
        )}
        <Button type="submit" disabled={mutation.isPending}>
          {text.submit}
        </Button>
        <p className="text-sm text-muted-foreground">
          {text.switchText}{' '}
          <Link to={text.switchLink.to} className="text-foreground underline">
            {text.switchLink.label}
          </Link>
        </p>
      </form>
    </main>
  )
}

function Field({
  name,
  label,
  error,
  ...props
}: { name: string; label: string; error: string | undefined } & ComponentProps<typeof Input>) {
  const errorId = `${name}-error`
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={name}>{label}</Label>
      <Input
        id={name}
        name={name}
        aria-invalid={error !== undefined}
        aria-describedby={error && errorId}
        {...props}
      />
      {error && (
        <p id={errorId} className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}
