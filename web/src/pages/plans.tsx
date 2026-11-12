import {
  type PlannedWorkoutInput,
  type PlanProposal,
  type PlanProposalItem,
  useCreatePlanProposal,
  useDecidePlanProposal,
  useLatestPlanProposal,
} from '@/api/plans'
import { problemMessage } from '@/api/problem'
import { Empty, QueryStatus } from '@/components/card'
import { PlanSteps } from '@/components/plan-steps'
import { Button } from '@/components/ui/button'
import { formatDay } from '@/lib/format/date'
import { sportLabels } from '@/lib/sports'

const failureMessages: Record<NonNullable<PlanProposal['failure']>, string> = {
  invalid_output: 'The coach’s plan was invalid twice, so nothing was proposed. Try again.',
  provider_error: 'The AI coach provider could not be reached. Try again.',
  not_configured: 'No AI coach provider is set up.',
  timeout: 'Planning took too long. Try again.',
}

const actionLabels: Record<PlanProposalItem['action'], string> = {
  add: 'Added',
  change: 'Changed',
  remove: 'Removed',
}

const statusLabels: Record<PlanProposalItem['status'], string> = {
  pending: 'Not decided',
  approved: 'Approved',
  rejected: 'Rejected',
}

export function PlansPage() {
  const latest = useLatestPlanProposal()
  const create = useCreatePlanProposal()
  const p = latest.data
  return (
    <div className="flex max-w-4xl flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold">Plans</h1>
        <Button
          disabled={create.isPending || p?.status === 'generating'}
          onClick={() => {
            create.mutate()
          }}
        >
          Plan the next 14 days
        </Button>
      </div>
      {create.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(create.error)}
        </p>
      )}
      <QueryStatus query={latest} />
      {latest.isSuccess && p === null && (
        <Empty>
          No plan yet. The coach plans from your races, load, readiness and injuries; you approve or
          reject each change before it applies.
        </Empty>
      )}
      {p && <Proposal proposal={p} />}
    </div>
  )
}

function Proposal({ proposal: p }: { proposal: PlanProposal }) {
  const decide = useDecidePlanProposal(p.id)
  const days = `${formatDay(p.from)} to ${formatDay(p.to)}`
  const pending = p.items.filter((it) => it.status === 'pending').map((it) => it.id)
  if (p.status === 'generating') return <p role="status">The coach is planning {days}…</p>
  if (p.status === 'failed') {
    return (
      <p role="alert" className="text-sm text-destructive">
        {failureMessages[p.failure ?? 'provider_error']}
      </p>
    )
  }
  if (p.items.length === 0) return <Empty>The coach proposed no changes for {days}.</Empty>
  return (
    <section aria-labelledby="proposal-heading" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 id="proposal-heading" className="text-lg font-semibold">
            Proposal for {days}
          </h2>
          {p.status === 'superseded' && (
            <p className="text-sm text-muted-foreground">A newer proposal replaced this one.</p>
          )}
        </div>
        {p.status === 'open' && pending.length > 0 && (
          <div className="flex gap-2">
            <Button
              disabled={decide.isPending}
              onClick={() => {
                decide.mutate({ approve: pending, reject: [] })
              }}
            >
              Approve all
            </Button>
            <Button
              variant="outline"
              disabled={decide.isPending}
              onClick={() => {
                decide.mutate({ approve: [], reject: pending })
              }}
            >
              Reject all
            </Button>
          </div>
        )}
      </div>
      {decide.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(decide.error)}
        </p>
      )}
      <ul className="flex flex-col gap-4">
        {p.items.map((it) => (
          <Item
            key={it.id}
            item={it}
            open={p.status === 'open'}
            busy={decide.isPending}
            onDecide={(approve) => {
              decide.mutate(
                approve ? { approve: [it.id], reject: [] } : { approve: [], reject: [it.id] },
              )
            }}
          />
        ))}
      </ul>
    </section>
  )
}

// Item shows one change as a diff: the workout as planned and the one proposed.
function Item({
  item: it,
  open,
  busy,
  onDecide,
}: {
  item: PlanProposalItem
  open: boolean
  busy: boolean
  onDecide: (approve: boolean) => void
}) {
  const w = it.after ?? it.before
  if (!w) return null // the API sends one or both
  const label = `${actionLabels[it.action]} · ${formatDay(w.day)}`
  return (
    <li aria-label={label} className="flex flex-col gap-3 rounded-md border p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="font-semibold">{label}</h3>
        {it.status === 'pending' && open ? (
          <div className="flex gap-2">
            <Button
              size="sm"
              disabled={busy}
              onClick={() => {
                onDecide(true)
              }}
            >
              Approve
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={busy}
              onClick={() => {
                onDecide(false)
              }}
            >
              Reject
            </Button>
          </div>
        ) : (
          <span className="text-sm text-muted-foreground">{statusLabels[it.status]}</span>
        )}
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        {it.before && <Workout heading="Before" workout={it.before} />}
        {it.after && <Workout heading={it.before ? 'After' : 'New'} workout={it.after} />}
      </div>
    </li>
  )
}

function Workout({ heading, workout: w }: { heading: string; workout: PlannedWorkoutInput }) {
  return (
    <div role="group" aria-label={heading} className="flex flex-col gap-2">
      <span className="text-xs font-medium uppercase text-muted-foreground">{heading}</span>
      <p className="font-medium">
        {w.title} · {sportLabels[w.sport]}
      </p>
      {w.description && <p className="text-sm whitespace-pre-wrap">{w.description}</p>}
      <PlanSteps steps={w.steps} sport={w.sport} />
    </div>
  )
}
