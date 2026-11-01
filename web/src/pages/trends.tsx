import { useMemo, useState } from 'react'
import type { ActivitySport } from '@/api/activities'
import { type Curves, useCurves, useVolume, type Volume } from '@/api/analytics'
import { Card, Empty, QueryStatus } from '@/components/card'
import { FitnessCard } from '@/components/fitness-card'
import { StreamChart } from '@/components/stream-chart'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { formatPeriod, periodStarts, toISODay } from '@/lib/format/date'
import {
  formatDistance,
  formatDuration,
  formatOneDecimal,
  formatPace,
  formatShare,
  formatWhole,
} from '@/lib/format/units'
import { zoneModelLabels } from '@/lib/format/zones'
import { sportLabels } from '@/lib/sports'

type Period = 'week' | 'month' | 'year'

const periods: { value: Period; label: string; count: number }[] = [
  { value: 'week', label: 'Last 12 weeks', count: 12 },
  { value: 'month', label: 'Last 12 months', count: 12 },
  { value: 'year', label: 'Last 10 years', count: 10 },
]

const metricLabels = { hr: 'Heart rate', pace: 'Pace', power: 'Power' } as const

export function TrendsPage() {
  const [period, setPeriod] = useState<Period>('month')
  const [sport, setSport] = useState<ActivitySport | ''>('')
  const today = toISODay(new Date())
  const count = periods.find((p) => p.value === period)?.count ?? 12
  const starts = periodStarts(period, today, count)
  const from = starts.at(-1) ?? today
  return (
    <div className="flex flex-col gap-8">
      <h1 className="text-2xl font-semibold">Trends</h1>
      <div className="flex flex-wrap items-end gap-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor="trends-period">Period</Label>
          <NativeSelect
            id="trends-period"
            value={period}
            onChange={(event) => {
              setPeriod(event.target.value as Period)
            }}
          >
            {periods.map(({ value, label }) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="trends-sport">Sport (volume and zones)</Label>
          <NativeSelect
            id="trends-sport"
            value={sport}
            onChange={(event) => {
              setSport(event.target.value as ActivitySport | '')
            }}
          >
            <NativeSelectOption value="">All sports</NativeSelectOption>
            {Object.entries(sportLabels).map(([value, label]) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
      </div>
      <div className="grid gap-8 lg:grid-cols-2">
        <VolumeCards period={period} starts={starts} from={from} to={today} sport={sport} />
        <FitnessCard from={from} to={today} />
        <CurvesCard from={from} to={today} />
      </div>
    </div>
  )
}

function VolumeCards({
  period,
  starts,
  from,
  to,
  sport,
}: {
  period: Period
  starts: string[]
  from: string
  to: string
  sport: ActivitySport | ''
}) {
  const volume = useVolume({ period, from, to, sport: sport || undefined })
  return (
    <>
      <Card title="Volume">
        <QueryStatus query={volume} />
        {volume.data?.periods.length === 0 && <Empty>No activities in this period.</Empty>}
        {volume.data && volume.data.periods.length > 0 && (
          <VolumeTable period={period} starts={starts} volume={volume.data} />
        )}
      </Card>
      <Card title="Time in zones">
        <QueryStatus query={volume} />
        {volume.data?.timeInZones.length === 0 && <Empty>No time in zones in this period.</Empty>}
        {volume.data?.timeInZones.map((z) => {
          const total = z.seconds.reduce((a, b) => a + b, 0)
          const title = `${metricLabels[z.metric]}: ${zoneModelLabels[z.model] ?? z.model}`
          return (
            <table
              key={`${z.metric}-${z.model}`}
              aria-label={title}
              className="w-full text-left text-sm"
            >
              <caption className="pb-1 text-left font-medium">{title}</caption>
              <tbody>
                {z.seconds.map((s, i) => (
                  <tr key={i} className="border-t border-border">
                    <td className="py-1">Zone {i + 1}</td>
                    <td className="py-1 text-right">{formatDuration(s)}</td>
                    <td className="py-1 text-right">{total > 0 ? formatShare(s / total) : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )
        })}
      </Card>
    </>
  )
}

function VolumeTable({
  period,
  starts,
  volume,
}: {
  period: Period
  starts: string[]
  volume: Volume
}) {
  const byStart = new Map(volume.periods.map((p) => [p.start, p]))
  return (
    <table aria-label="Volume" className="w-full text-left text-sm">
      <thead className="text-muted-foreground">
        <tr>
          <th className="py-2 font-medium">Period</th>
          <th className="py-2 text-right font-medium">Activities</th>
          <th className="py-2 text-right font-medium">Duration</th>
          <th className="py-2 text-right font-medium">Distance</th>
          <th className="py-2 text-right font-medium">Ascent</th>
          <th className="py-2 text-right font-medium">Load</th>
        </tr>
      </thead>
      <tbody>
        {starts.map((start) => {
          const p = byStart.get(start)
          return (
            <tr key={start} className="border-t border-border">
              <td className="py-2">{formatPeriod(period, start)}</td>
              <td className="py-2 text-right">{p?.activities ?? 0}</td>
              <td className="py-2 text-right">{formatDuration(p?.durationS ?? 0)}</td>
              <td className="py-2 text-right">{formatDistance(p?.distanceM ?? 0)}</td>
              <td className="py-2 text-right">{formatWhole(p?.ascentM ?? 0, 'm')}</td>
              <td className="py-2 text-right">{Math.round(p?.load ?? 0)}</td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

function CurvesCard({ from, to }: { from: string; to: string }) {
  const curves = useCurves(from, to)
  const c = curves.data
  return (
    <Card title="Curves">
      <QueryStatus query={curves} />
      {c && c.power.length + c.pace.length === 0 && (
        <Empty>No power or pace efforts in this period.</Empty>
      )}
      {c && c.power.length > 0 && (
        <CurveChart label="Power curve" points={c.power} format={(v) => formatWhole(v, 'W')} />
      )}
      {c && c.pace.length > 0 && (
        <CurveChart label="Pace curve" points={c.pace} format={(v) => formatPace(v)} invert />
      )}
      {c && <Estimates curves={c} />}
    </Card>
  )
}

// CurveChart spaces the curve's durations evenly, as they grow roughly geometrically.
function CurveChart({
  label,
  points,
  format,
  invert,
}: {
  label: string
  points: Curves['power']
  format: (v: number) => string
  invert?: boolean
}) {
  const chart = useMemo(
    () => ({
      index: points.map((_, i) => i),
      values: points.map((p) => p.value),
      durationAt: (i: number) => formatDuration(points[Math.round(i)]?.durationS ?? 0),
    }),
    [points],
  )
  return (
    <StreamChart
      label={label}
      time={chart.index}
      values={chart.values}
      formatX={chart.durationAt}
      format={format}
      invert={invert}
    />
  )
}

function Estimates({ curves: c }: { curves: Curves }) {
  const rows: [string, string][] = []
  if (c.ftpW !== undefined) rows.push(['Estimated FTP', formatWhole(c.ftpW, 'W')])
  if (c.thresholdSpeedMS !== undefined)
    rows.push(['Threshold pace', formatPace(c.thresholdSpeedMS)])
  if (c.vo2max !== undefined) rows.push(['VO2max', formatOneDecimal(c.vo2max, 'ml/kg/min')])
  for (const r of c.racePredictions) {
    rows.push([`Predicted ${formatDistance(r.distanceM)}`, formatDuration(r.timeS)])
  }
  if (c.cssMS !== undefined) rows.push(['Critical swim speed', formatPace(c.cssMS, 100)])
  if (rows.length === 0) return null
  return (
    <dl aria-label="Estimates" className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
      {rows.map(([label, value]) => (
        <div key={label} className="contents">
          <dt className="text-muted-foreground">{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  )
}
