import { useMemo, useState } from 'react'
import { Link, useParams } from 'react-router'
import {
  type ActivityComparison,
  type ActivityDetail,
  type ActivitySport,
  type ComparisonAxis,
  useActivity,
  useComparison,
} from '@/api/activities'
import { problemMessage } from '@/api/problem'
import { StreamChart } from '@/components/stream-chart'
import { Button } from '@/components/ui/button'
import {
  formatActivityStart,
  formatDistance,
  formatDuration,
  formatWhole,
} from '@/lib/format/units'
import { charts, dash, pacePer, speedText } from '@/lib/activity-display'
import { sportLabels } from '@/lib/sports'

// ActivityComparePage overlays two activities' charts, aligned by time or distance,
// with their summaries side by side.
export function ActivityComparePage() {
  const params = useParams()
  const a = useActivity(Number(params.id))
  const b = useActivity(Number(params.other))

  if (a.isPending || b.isPending) return <p className="text-sm text-muted-foreground">Loading…</p>
  if (a.isError || b.isError) {
    return (
      <p role="alert" className="text-sm text-destructive">
        {problemMessage(a.error ?? b.error)}
      </p>
    )
  }
  return <Comparison a={a.data} b={b.data} />
}

function Comparison({ a, b }: { a: ActivityDetail; b: ActivityDetail }) {
  const byDistance = a.distanceM !== undefined && b.distanceM !== undefined
  const [axis, setAxis] = useState<ComparisonAxis>(byDistance ? 'distance' : 'time')
  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-1">
        <Link
          to={`/activities/${String(a.id)}`}
          className="text-sm text-muted-foreground underline"
        >
          Back to the activity
        </Link>
        <h1 className="text-2xl font-semibold">Comparison</h1>
      </header>
      <SideBySide a={a} b={b} />
      <div role="group" aria-label="Align by" className="flex gap-2">
        {(['time', 'distance'] as const).map((value) => (
          <Button
            key={value}
            variant={axis === value ? 'default' : 'outline'}
            size="sm"
            aria-pressed={axis === value}
            disabled={value === 'distance' && !byDistance}
            onClick={() => {
              setAxis(value)
            }}
          >
            {value === 'time' ? 'By time' : 'By distance'}
          </Button>
        ))}
      </div>
      <Charts a={a} b={b} axis={axis} />
    </div>
  )
}

function SideBySide({ a, b }: { a: ActivityDetail; b: ActivityDetail }) {
  const pace = pacePer(a.sport) !== null && pacePer(b.sport) !== null
  const rows: [string, (x: ActivityDetail) => string][] = [
    ['Sport', (x) => sportLabels[x.sport]],
    ['Distance', (x) => dash(x.distanceM, formatDistance)],
    ['Duration', (x) => dash(x.timerTimeS ?? x.elapsedTimeS, formatDuration)],
    [pace ? 'Avg pace' : 'Avg speed', (x) => speedText(x.sport, x.avgSpeedMS)],
    ['Avg heart rate', (x) => dash(x.avgHrBpm, (v) => formatWhole(v, 'bpm'))],
    ['Avg power', (x) => dash(x.avgPowerW, (v) => formatWhole(v, 'W'))],
    ['Ascent', (x) => dash(x.ascentM, (v) => formatWhole(v, 'm'))],
  ]
  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="text-left text-muted-foreground">
          <td />
          {[a, b].map((x, i) => (
            <th key={x.id} scope="col" className="font-medium">
              <Link to={`/activities/${String(x.id)}`} className="underline">
                {formatActivityStart(x.startTime, x.utcOffsetS)}
              </Link>
              <span className="text-muted-foreground"> ({i === 0 ? 'solid' : 'dashed'})</span>
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map(([label, value]) => (
          <tr key={label}>
            <th scope="row" className="py-1 text-left font-normal text-muted-foreground">
              {label}
            </th>
            <td>{value(a)}</td>
            <td>{value(b)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function Charts({ a, b, axis }: { a: ActivityDetail; b: ActivityDetail; axis: ComparisonAxis }) {
  const cmp = useComparison(a.id, b.id, axis)
  // Stable across renders so the charts are only rebuilt for new data.
  const series = useMemo(() => (cmp.data ? overlay(a.sport, cmp.data) : []), [a.sport, cmp.data])

  if (cmp.isPending) return <p className="text-sm text-muted-foreground">Loading charts…</p>
  if (cmp.isError) {
    return (
      <p role="alert" className="text-sm text-destructive">
        {problemMessage(cmp.error)}
      </p>
    )
  }
  if (series.length === 0) {
    return <p className="text-sm text-muted-foreground">Neither activity has samples to chart.</p>
  }
  const formatX = axis === 'distance' ? formatDistance : formatDuration
  return (
    <div className="flex flex-col gap-4">
      {series.map((c) => (
        <StreamChart key={c.label} {...c} time={cmp.data.x} formatX={formatX} />
      ))}
    </div>
  )
}

// overlay pairs each chart of a with the same chart of b, both in a's units;
// a channel only one of them recorded is charted with the other empty.
function overlay(sport: ActivitySport, c: ActivityComparison) {
  const empty = c.x.map(() => null)
  const theirs = charts(sport, c.b)
  const out = charts(sport, c.a).map((mine) => ({
    ...mine,
    compare: theirs.find((s) => s.label === mine.label)?.values ?? empty,
  }))
  for (const other of theirs) {
    if (!out.some((s) => s.label === other.label)) {
      out.push({ ...other, values: empty, compare: other.values })
    }
  }
  return out
}
