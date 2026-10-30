import type { Zone } from '@/api/zones'
import { formatPace } from '@/lib/format/units'

export type ZoneMetric = 'hr' | 'pace' | 'power'

export const zoneModelLabels: Record<string, string> = {
  friel_run_hr: 'Friel, running heart rate',
  friel_bike_hr: 'Friel, cycling heart rate',
  heart_rate_reserve: 'Heart rate reserve',
  friel_run_pace: 'Friel, running pace',
  css: 'CSS pace',
  coggan_power: 'Coggan power',
}

// zoneRange renders a zone's [low, high) range: whole bpm or W, or paces from
// slow to fast (per 1000 m on foot, per 100 m in the water).
export function zoneRange(metric: ZoneMetric, zone: Zone, per = 1000): string {
  const { low, high } = zone
  if (metric === 'pace') {
    if (low === undefined && high !== undefined) return `slower than ${formatPace(high, per)}`
    if (high === undefined && low !== undefined) return `${formatPace(low, per)} or faster`
    if (low === undefined || high === undefined) return '—'
    return `${formatPace(low, per)} – ${formatPace(high, per)}`
  }
  const unit = metric === 'hr' ? 'bpm' : 'W'
  const whole = (v: number) => String(Math.round(v))
  if (low === undefined && high !== undefined) return `below ${whole(high)} ${unit}`
  if (high === undefined && low !== undefined) return `${whole(low)} ${unit} and above`
  if (low === undefined || high === undefined) return '—'
  return `${whole(low)}–${whole(high)} ${unit}`
}
