import { useParams } from 'react-router'
import { usePlannedWorkout } from '@/api/plans'
import { QueryStatus } from '@/components/card'
import { PlanSteps } from '@/components/plan-steps'
import { formatDay } from '@/lib/format/date'
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
