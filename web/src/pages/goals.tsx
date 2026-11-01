import { useState, type SubmitEvent } from 'react'
import type { ActivitySport } from '@/api/activities'
import { type Goal, type GoalInput, useCreateGoal, useDeleteGoal, useGoals } from '@/api/goals'
import { problemMessage } from '@/api/problem'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { formatDay, formatPeriod, toISODay } from '@/lib/format/date'
import { formatShare } from '@/lib/format/units'
import {
  formatGoalValue,
  metricLabels,
  parseGoalTarget,
  statusLabels,
  targetUnits,
} from '@/lib/goals'
import { sportLabels } from '@/lib/sports'

const periodLabels: Record<GoalInput['period'], string> = {
  week: 'Week',
  month: 'Month',
  year: 'Year',
}

export function GoalsPage() {
  const goals = useGoals()
  const items = goals.data?.pages.flatMap((page) => page.items) ?? []
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold">Goals</h1>
        <GoalDialog />
      </div>
      {goals.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
      {goals.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(goals.error)}
        </p>
      )}
      {goals.isSuccess && items.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No goals yet. Set a distance, time or ascent target for a week, month or year.
        </p>
      )}
      {items.length > 0 && (
        <ul className="grid gap-4 lg:grid-cols-2">
          {items.map((g) => (
            <GoalItem key={g.id} goal={g} />
          ))}
        </ul>
      )}
      {goals.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={goals.isFetchingNextPage}
          onClick={() => {
            void goals.fetchNextPage()
          }}
        >
          Show more
        </Button>
      )}
    </div>
  )
}

function GoalItem({ goal: g }: { goal: Goal }) {
  const remove = useDeleteGoal()
  const title = `${formatGoalValue(g.metric, g.target)} ${g.sport ? sportLabels[g.sport].toLowerCase() : 'in all sports'}`
  const when = formatPeriod(g.period, g.start)
  const share = Math.min(g.value / g.target, 1)
  return (
    <li aria-label={`${title}, ${when}`} className="flex flex-col gap-2 rounded-md border p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="font-semibold">{title}</h2>
          <p className="text-sm text-muted-foreground">
            {metricLabels[g.metric]} · {when}
          </p>
        </div>
        <span className="text-sm font-medium">{statusLabels[g.status]}</span>
      </div>
      <div
        role="progressbar"
        aria-label="Progress"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(share * 100)}
        className="h-2 overflow-hidden rounded-full bg-muted"
      >
        <div className="h-full bg-primary" style={{ width: `${String(share * 100)}%` }} />
      </div>
      <p className="text-sm">
        {formatGoalValue(g.metric, g.value)} of {formatGoalValue(g.metric, g.target)} (
        {formatShare(g.value / g.target)})
      </p>
      {(g.status === 'ahead' || g.status === 'behind') && (
        <p className="text-sm text-muted-foreground">
          Projected {formatGoalValue(g.metric, g.projected)} by {formatDay(g.end)}
          {g.projectedFinish && `, reaching the target on ${formatDay(g.projectedFinish)}`}.
        </p>
      )}
      <Button
        variant="ghost"
        size="sm"
        className="self-end"
        aria-label={`Delete ${title}, ${when}`}
        disabled={remove.isPending}
        onClick={() => {
          remove.mutate(g.id)
        }}
      >
        Delete
      </Button>
      {remove.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(remove.error)}
        </p>
      )}
    </li>
  )
}

function GoalDialog() {
  const [open, setOpen] = useState(false)
  const [metric, setMetric] = useState<GoalInput['metric']>('distance')
  const [invalid, setInvalid] = useState<string | null>(null)
  const create = useCreateGoal()

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const field = (name: string) => {
      const value = form.get(name)
      return typeof value === 'string' ? value.trim() : ''
    }
    const target = parseGoalTarget(metric, field('target'))
    if (target === null) {
      setInvalid(`Enter a target in ${targetUnits[metric]}.`)
      return
    }
    if (field('day') === '') {
      setInvalid('Pick a day in the period.')
      return
    }
    setInvalid(null)
    const sport = field('sport')
    create.mutate(
      {
        period: field('period') as GoalInput['period'],
        start: field('day'),
        metric,
        target,
        ...(sport !== '' && { sport: sport as ActivitySport }),
      },
      {
        onSuccess: () => {
          setOpen(false)
        },
      },
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        create.reset()
        setInvalid(null)
      }}
    >
      <DialogTrigger asChild>
        <Button>Add goal</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add goal</DialogTitle>
          <DialogDescription>
            Progress adds up from your activities in the week, month or year you pick.
          </DialogDescription>
        </DialogHeader>
        <form noValidate onSubmit={onSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="goal-period">Period</Label>
            <NativeSelect id="goal-period" name="period" defaultValue="year">
              {Object.entries(periodLabels).map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="goal-day">Any day in the period</Label>
            <Input id="goal-day" name="day" type="date" defaultValue={toISODay(new Date())} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="goal-sport">Sport</Label>
            <NativeSelect id="goal-sport" name="sport" defaultValue="">
              <NativeSelectOption value="">All sports</NativeSelectOption>
              {Object.entries(sportLabels).map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="goal-metric">Measure</Label>
            <NativeSelect
              id="goal-metric"
              value={metric}
              onChange={(event) => {
                setMetric(event.target.value as GoalInput['metric'])
              }}
            >
              {Object.entries(metricLabels).map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="goal-target">Target ({targetUnits[metric]})</Label>
            <Input id="goal-target" name="target" inputMode="decimal" />
          </div>
          {(invalid ?? create.isError) && (
            <p role="alert" className="text-sm text-destructive">
              {invalid ?? problemMessage(create.error)}
            </p>
          )}
          <Button type="submit" className="self-end" disabled={create.isPending}>
            Save
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  )
}
