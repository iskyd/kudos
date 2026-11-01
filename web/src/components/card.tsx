import type { ReactNode } from 'react'
import { problemMessage } from '@/api/problem'

// Card is a titled section of a page; its title names the region.
export function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="flex flex-col gap-3">
      <h2 className="text-lg font-semibold">{title}</h2>
      {children}
    </section>
  )
}

// QueryStatus renders a query's loading and error states.
export function QueryStatus({
  query,
}: {
  query: { isPending: boolean; isError: boolean; error: unknown }
}) {
  if (query.isPending) return <p className="text-sm text-muted-foreground">Loading…</p>
  if (query.isError) {
    return (
      <p role="alert" className="text-sm text-destructive">
        {problemMessage(query.error)}
      </p>
    )
  }
  return null
}

// Empty is the note a card shows without data.
export function Empty({ children }: { children: ReactNode }) {
  return <p className="text-sm text-muted-foreground">{children}</p>
}
