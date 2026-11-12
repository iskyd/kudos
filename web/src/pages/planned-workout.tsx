import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { useActivities } from '@/api/activities'
import { type PlannedWorkout, usePlannedWorkout, usePlannedWorkoutLink } from '@/api/plans'
import { problemMessage } from '@/api/problem'
import { QueryStatus } from '@/components/card'
import { PlanSteps } from '@/components/plan-steps'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { formatDay } from '@/lib/format/date'
import { formatDuration, formatLocalClock } from '@/lib/format/units'
import { sportLabels } from '@/lib/sports'

export function PlannedWorkoutPage() {
  const id = Number(useParams().id)
  const workout = usePlannedWorkout(id)
  const w = workout.data
  return (
    <div className="flex max-w-2xl flex-col gap-6">
      <QueryStatus query={workout} />
      {w && (
        <>
          <div className="flex flex-col gap-1">
            <h1 className="text-2xl font-semibold">{w.title}</h1>
            <p className="text-sm text-muted-foreground">
              Planned for {formatDay(w.day)} · {sportLabels[w.sport]}
            </p>
          </div>
          {w.description && <p className="whitespace-pre-wrap">{w.description}</p>}
          <Completion workout={w} />
          <section aria-labelledby="steps-heading" className="flex flex-col gap-2">
            <h2 id="steps-heading" className="text-lg font-semibold">
              Steps
            </h2>
            <PlanSteps steps={w.steps} sport={w.sport} />
          </section>
        </>
      )}
    </div>
  )
}

// Completion shows the activity that completed the workout and lets the user
// link another of the day's activities, unlink it, or go back to automatic matching.
function Completion({ workout: w }: { workout: PlannedWorkout }) {
  const link = usePlannedWorkoutLink(w.id)
  const activities = useActivities({ from: w.day, to: w.day, sort: 'date' })
  const others = (activities.data?.pages.flatMap((p) => p.items) ?? []).filter(
    (a) => a.id !== w.activityId,
  )
  const [picked, setPicked] = useState('')
  const manual = w.link === 'manual'
  return (
    <section aria-labelledby="completion-heading" className="flex flex-col gap-3">
      <h2 id="completion-heading" className="text-lg font-semibold">
        Completion
      </h2>
      {w.activityId === undefined ? (
        <p className="text-sm">
          {manual ? 'Unlinked: no activity completes it.' : 'Not completed yet.'}
        </p>
      ) : (
        <p className="text-sm">
          Completed by{' '}
          <Link to={`/activities/${String(w.activityId)}`} className="underline">
            this activity
          </Link>
          {manual ? ', linked by you.' : ', matched automatically.'}
        </p>
      )}
      {others.length > 0 && (
        <div className="flex flex-wrap items-end gap-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="link-activity">Activity of the day</Label>
            <NativeSelect
              id="link-activity"
              value={picked}
              onChange={(event) => {
                setPicked(event.target.value)
              }}
            >
              <NativeSelectOption value="">Choose…</NativeSelectOption>
              {others.map((a) => (
                <NativeSelectOption key={a.id} value={String(a.id)}>
                  {[
                    formatLocalClock(a.startTime, a.utcOffsetS).slice(0, 5),
                    sportLabels[a.sport],
                    formatDuration(a.timerTimeS ?? a.elapsedTimeS ?? 0),
                  ].join(' · ')}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <Button
            disabled={picked === '' || link.isPending}
            onClick={() => {
              link.mutate(Number(picked))
            }}
          >
            Link
          </Button>
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        {w.activityId !== undefined && (
          <Button
            variant="outline"
            size="sm"
            disabled={link.isPending}
            onClick={() => {
              link.mutate(null)
            }}
          >
            Unlink
          </Button>
        )}
        {manual && (
          <Button
            variant="ghost"
            size="sm"
            disabled={link.isPending}
            onClick={() => {
              link.mutate('auto')
            }}
          >
            Match automatically
          </Button>
        )}
      </div>
      {link.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(link.error)}
        </p>
      )}
    </section>
  )
}
