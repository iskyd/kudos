import { problemMessage } from '@/api/problem'
import { type Readiness, useReadiness } from '@/api/wellness'
import { formatDuration, formatShare, formatWhole } from '@/lib/format/units'

type Component = Readiness['components'][number]

const componentLabels: Record<Component['name'], string> = {
  hrv: 'HRV',
  resting_hr: 'Resting HR',
  sleep: 'Sleep',
  form: 'Form',
  check_in: 'Check-in',
}

const capLabels: Record<NonNullable<Readiness['cap']>['kind'], string> = {
  illness: 'an illness',
  injury: 'an injury',
  period: 'a period',
}

function formatValue(name: Component['name'], value: number | undefined): string {
  if (value === undefined) return '—'
  switch (name) {
    case 'hrv':
      return formatWhole(value, 'ms')
    case 'resting_hr':
      return formatWhole(value, 'bpm')
    case 'sleep':
      return formatDuration(value)
    default:
      return formatWhole(value, 'TSB')
  }
}

// ReadinessCard shows a day's readiness score with its breakdown.
export function ReadinessCard({ day }: { day: string }) {
  const readiness = useReadiness(day)

  return (
    <section aria-label="Readiness" className="flex flex-col gap-3">
      <h2 className="text-lg font-semibold">Readiness</h2>
      {readiness.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
      {readiness.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(readiness.error)}
        </p>
      )}
      {readiness.data?.status === 'collecting_baseline' && (
        <p className="text-sm text-muted-foreground">
          Collecting baseline: {readiness.data.baselineDays} of {readiness.data.requiredDays} days
          of HRV and resting HR so far.
        </p>
      )}
      {readiness.data?.status === 'ready' && <Breakdown readiness={readiness.data} />}
    </section>
  )
}

function Breakdown({ readiness }: { readiness: Readiness }) {
  const { score, uncappedScore, cap, components } = readiness
  return (
    <>
      <p>
        <span className="text-4xl font-semibold">{score}</span>
        <span className="text-muted-foreground"> / 100</span>
      </p>
      {cap && (
        <p role="note" className="text-sm text-muted-foreground">
          {`Capped at ${String(cap.max)} by ${capLabels[cap.kind]} in the health log`}
          {uncappedScore !== score && ` (${String(uncappedScore)} without it)`}.
        </p>
      )}
      <table aria-label="Readiness breakdown" className="w-full text-left text-sm">
        <thead className="text-muted-foreground">
          <tr>
            <th className="py-2 font-medium">Component</th>
            <th className="py-2 text-right font-medium">Today</th>
            <th className="py-2 text-right font-medium">60-day baseline</th>
            <th className="py-2 text-right font-medium">Score</th>
            <th className="py-2 text-right font-medium">Weight</th>
          </tr>
        </thead>
        <tbody>
          {components.map((c) => (
            <tr key={c.name} className="border-t border-border">
              <td className="py-2">{componentLabels[c.name]}</td>
              <td className="py-2 text-right">{formatValue(c.name, c.value)}</td>
              <td className="py-2 text-right">{formatValue(c.name, c.baseline)}</td>
              <td className="py-2 text-right">{Math.round(c.score)}</td>
              <td className="py-2 text-right">{formatShare(c.share)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  )
}
