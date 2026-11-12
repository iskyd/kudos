import type { ActivitySport } from '@/api/activities'
import { type PlanStep, stepText } from '@/lib/plans'

// PlanSteps lists steps in order; a repeat lists its own steps under "n ×".
export function PlanSteps({ steps, sport }: { steps: PlanStep[]; sport: ActivitySport }) {
  return (
    <ol className="flex flex-col gap-2 border-l border-border pl-4">
      {steps.map((s, i) =>
        s.kind === 'repeat' ? (
          <li key={i} aria-label={`${String(s.count)} times`} className="flex flex-col gap-2">
            <span className="font-medium">{s.count} ×</span>
            <PlanSteps steps={s.steps ?? []} sport={sport} />
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
