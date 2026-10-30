import { type ActivityDetail, type WorkoutExecution, useExecution } from '@/api/activities'
import { problemMessage } from '@/api/problem'
import { dash, resultText, targetRange, targetValue } from '@/lib/activity-display'
import { formatDistance, formatDuration, formatWhole } from '@/lib/format/units'

// WorkoutExecutionPanel shows how a structured workout went against its targets:
// each workout step's run, or for a Zwift-style workout its target power intervals.
export function WorkoutExecutionPanel({ a }: { a: ActivityDetail }) {
  const execution = useExecution(a.id)
  if (execution.isPending) {
    return <p className="text-sm text-muted-foreground">Loading workout execution…</p>
  }
  if (execution.isError) {
    return (
      <p role="alert" className="text-sm text-destructive">
        {problemMessage(execution.error)}
      </p>
    )
  }
  const e = execution.data
  if (e.kind === 'none') return null
  return (
    <section aria-label="Workout execution" className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">Workout execution</h2>
      {e.kind === 'steps' ? <Steps a={a} e={e} /> : <Intervals e={e} />}
    </section>
  )
}

function Steps({ a, e }: { a: ActivityDetail; e: WorkoutExecution }) {
  const steps = a.workout?.steps ?? []
  return (
    <Table
      label="Workout steps"
      head={['Step', 'Laps', 'Duration', 'Distance', 'Target', 'Actual', 'Result']}
      rows={(e.steps ?? []).map((r) => {
        const step = steps.find((s) => s.index === r.stepIndex)
        const type = step?.targetType
        return [
          step?.name ?? step?.intensity ?? `Step ${String(r.stepIndex + 1)}`,
          String(r.laps),
          dash(r.timerTimeS, formatDuration),
          dash(r.distanceM, formatDistance),
          step ? targetRange(a.sport, step) : '—',
          r.actual === undefined || type === undefined ? '—' : targetValue(a.sport, type, r.actual),
          resultText(a.sport, type, r.result),
        ]
      })}
    />
  )
}

function Intervals({ e }: { e: WorkoutExecution }) {
  const share = e.totalS ? Math.round((100 * (e.withinS ?? 0)) / e.totalS) : null
  const tolerance = formatWhole(e.toleranceW ?? 0, 'W')
  return (
    <>
      <p className="text-sm">
        {share === null
          ? 'No power was recorded during the workout.'
          : `${String(share)}% of the time within ${tolerance} of the target power.`}
      </p>
      <Table
        label="Intervals"
        head={['Start', 'Duration', 'Target', 'Avg power', 'On target']}
        rows={(e.intervals ?? []).map((iv) => [
          formatDuration(iv.startS),
          formatDuration(iv.durationS),
          formatWhole(iv.targetW, 'W'),
          dash(iv.avgPowerW, (v) => formatWhole(v, 'W')),
          `${String(Math.round((100 * iv.withinS) / iv.durationS))}%`,
        ])}
      />
    </>
  )
}

function Table({ label, head, rows }: { label: string; head: string[]; rows: string[][] }) {
  return (
    <table aria-label={label} className="w-full text-sm">
      <thead>
        <tr className="text-left text-muted-foreground">
          {head.map((h) => (
            <th key={h} scope="col" className="font-normal">
              {h}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((cells, i) => (
          <tr key={i}>
            {cells.map((c, j) => (
              <td key={j} className="py-1">
                {c}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}
