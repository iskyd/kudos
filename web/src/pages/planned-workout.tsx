import { useParams } from 'react-router'
import type { ActivitySport } from '@/api/activities'
import { usePlannedWorkout } from '@/api/plans'
import { QueryStatus } from '@/components/card'
import { formatDay } from '@/lib/format/date'
import { type PlanStep, stepText } from '@/lib/plans'
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
            <Steps steps={w.steps} sport={w.sport} />
          </section>
        </>
      )}
    </div>
  )
}

// Steps lists steps in order; a repeat lists its own steps under "n ×".
function Steps({ steps, sport }: { steps: PlanStep[]; sport: ActivitySport }) {
  return (
    <ol className="flex flex-col gap-2 border-l border-border pl-4">
      {steps.map((s, i) =>
        s.kind === 'repeat' ? (
          <li key={i} aria-label={`${String(s.count)} times`} className="flex flex-col gap-2">
            <span className="font-medium">{s.count} ×</span>
            <Steps steps={s.steps ?? []} sport={sport} />
          </li>
        ) : (
          <li key={i} className="flex flex-col">
            <span>{stepText(s, sport)}</span>
            {s.note && <span className="text-sm text-muted-foreground">{s.note}</span>}
          </li>
        ),
      )}
    </ol>
  )
}
