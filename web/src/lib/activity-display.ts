import type { ActivitySport, ActivityStreams, StepExecution, WorkoutStep } from '@/api/activities'
import {
  formatDuration,
  formatPace,
  formatSpeed,
  formatWhole,
  paceSeconds,
} from '@/lib/format/units'

// Pace is shown per km on foot and per 100 m in the water; other sports show speed.
export function pacePer(sport: ActivitySport): number | null {
  switch (sport) {
    case 'running':
    case 'trail_running':
    case 'hiking':
      return 1000
    case 'pool_swimming':
    case 'open_water_swimming':
      return 100
    default:
      return null
  }
}

export function speedText(sport: ActivitySport, speed: number | undefined): string {
  if (speed === undefined) return '—'
  const per = pacePer(sport)
  return per === null ? formatSpeed(speed) : formatPace(speed, per)
}

export const dash = (v: number | undefined, f: (v: number) => string) =>
  v === undefined ? '—' : f(v)

// Paces slower than this (stops, standing at lights) would flatten the pace chart.
const slowestChartPace = 30 * 60

// charts lists the channels to chart, in order, with SI values formatted for display.
export function charts(
  sport: ActivitySport,
  s: Pick<ActivityStreams, 'hrBpm' | 'speedMS' | 'powerW' | 'cadence' | 'altitudeM'>,
) {
  const out: {
    label: string
    values: (number | null)[]
    format: (v: number) => string
    invert?: boolean
  }[] = []
  if (s.hrBpm)
    out.push({ label: 'Heart rate', values: s.hrBpm, format: (v) => formatWhole(v, 'bpm') })
  const per = pacePer(sport)
  if (s.speedMS && per === null) {
    out.push({ label: 'Speed', values: s.speedMS, format: formatSpeed })
  } else if (s.speedMS && per !== null) {
    const values = s.speedMS.map((v) => {
      const p = v === null ? null : paceSeconds(v, per)
      return p === null || p > slowestChartPace ? null : p
    })
    out.push({ label: 'Pace', values, format: formatDuration, invert: true })
  }
  if (s.powerW) out.push({ label: 'Power', values: s.powerW, format: (v) => formatWhole(v, 'W') })
  if (s.cadence) {
    const unit = sport === 'cycling' ? 'rpm' : 'spm'
    out.push({ label: 'Cadence', values: s.cadence, format: (v) => formatWhole(v, unit) })
  }
  if (s.altitudeM) {
    out.push({ label: 'Elevation', values: s.altitudeM, format: (v) => formatWhole(v, 'm') })
  }
  return out
}

// Targets the execution panel compares; others (zones, percentages, cadence) are shown as recorded.
const compared = ['speed', 'heart_rate', 'power']

// targetValue renders one value of a step's target type: speed as pace on foot.
export function targetValue(sport: ActivitySport, type: string, v: number): string {
  switch (type) {
    case 'speed':
      return speedText(sport, v)
    case 'heart_rate':
      return formatWhole(v, 'bpm')
    case 'power':
      return formatWhole(v, 'W')
    default:
      return '—'
  }
}

// targetRange renders a step's target as a range, fastest pace first on foot.
export function targetRange(sport: ActivitySport, step: WorkoutStep): string {
  const { targetType: type, targetLow: lo, targetHigh: hi } = step
  if (type === undefined || lo === undefined || hi === undefined || !compared.includes(type)) {
    return '—'
  }
  let [a, b] = [Math.min(lo, hi), Math.max(lo, hi)]
  if (type === 'speed' && pacePer(sport) !== null) [a, b] = [b, a]
  return `${targetValue(sport, type, a)} – ${targetValue(sport, type, b)}`
}

// resultText says how a run went against its target; on foot, a speed above the target is faster.
export function resultText(
  sport: ActivitySport,
  type: string | undefined,
  r: StepExecution['result'],
) {
  const pace = type === 'speed' && pacePer(sport) !== null
  switch (r) {
    case 'skipped':
      return 'Skipped'
    case 'within':
      return 'On target'
    case 'above':
      return pace ? 'Faster' : 'Above'
    case 'below':
      return pace ? 'Slower' : 'Below'
    default:
      return '—'
  }
}
