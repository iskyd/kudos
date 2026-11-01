import { useMemo } from 'react'
import { useFitness } from '@/api/analytics'
import { Card, Empty, QueryStatus } from '@/components/card'
import { StreamChart } from '@/components/stream-chart'
import { formatDay } from '@/lib/format/date'

const whole = (v: number) => String(Math.round(v))

// FitnessCard shows the last day's fitness, fatigue and form with their trend
// over the local days from from to to.
export function FitnessCard({ from, to }: { from: string; to: string }) {
  const fitness = useFitness(from, to)
  const chart = useMemo(() => {
    const days = fitness.data ?? []
    return {
      days,
      index: days.map((_, i) => i),
      ctl: days.map((d) => d.ctl),
      atl: days.map((d) => d.atl),
      tsb: days.map((d) => d.tsb),
      dayAt: (i: number) => formatDay(days[Math.round(i)]?.day ?? to),
    }
  }, [fitness.data, to])
  const last = chart.days.at(-1)
  const trained = chart.days.some((d) => d.ctl > 0 || d.atl > 0)
  return (
    <Card title="Fitness and form">
      <QueryStatus query={fitness} />
      {fitness.isSuccess && !trained && <Empty>No training load in this period.</Empty>}
      {last && trained && (
        <>
          <dl className="grid grid-cols-3 gap-2 text-sm">
            {(
              [
                ['Fitness', last.ctl],
                ['Fatigue', last.atl],
                ['Form', last.tsb],
              ] as const
            ).map(([label, value]) => (
              <div key={label}>
                <dt className="text-muted-foreground">{label}</dt>
                <dd className="text-2xl font-semibold">{whole(value)}</dd>
              </div>
            ))}
          </dl>
          <StreamChart
            label="Fitness (fatigue dashed)"
            time={chart.index}
            values={chart.ctl}
            compare={chart.atl}
            formatX={chart.dayAt}
            format={whole}
          />
          <StreamChart
            label="Form"
            time={chart.index}
            values={chart.tsb}
            formatX={chart.dayAt}
            format={whole}
          />
        </>
      )}
    </Card>
  )
}
