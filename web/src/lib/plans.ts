import type { ActivitySport } from '@/api/activities'
import type { components } from '@/api/generated/schema'
import { pacePer } from '@/lib/activity-display'
import { formatDistance, formatDuration, formatPaceRange, formatSpeed } from '@/lib/format/units'

export type PlanStep = components['schemas']['PlanStep']
export type PlanTarget = components['schemas']['PlanTarget']

export const stepKindLabels: Record<PlanStep['kind'], string> = {
  warmup: 'Warm-up',
  interval: 'Interval',
  recovery: 'Recovery',
  cooldown: 'Cool-down',
  repeat: 'Repeat',
}

const targetLabels: Record<PlanTarget['metric'], string> = {
  heart_rate: 'HR',
  pace: 'Pace',
  power: 'Power',
}

// targetText renders a target as a zone ("HR zone 2") or a range in the sport's units.
export function targetText(t: PlanTarget, sport: ActivitySport): string {
  if (t.zone !== undefined) return `${targetLabels[t.metric]} zone ${String(t.zone)}`
  const low = t.low ?? 0
  const high = t.high ?? 0
  switch (t.metric) {
    case 'heart_rate':
      return `${String(Math.round(low))}–${String(Math.round(high))} bpm`
    case 'power':
      return `${String(Math.round(low))}–${String(Math.round(high))} W`
    case 'pace': {
      const per = pacePer(sport)
      return per === null
        ? `${formatSpeed(low)}–${formatSpeed(high)}`
        : formatPaceRange(low, high, per)
    }
  }
}

// stepText renders a step that is not a repeat: "Interval · 1.00 km · 4:04–4:16 /km".
export function stepText(step: PlanStep, sport: ActivitySport): string {
  const duration =
    step.durationType === 'time'
      ? formatDuration(step.duration ?? 0)
      : step.durationType === 'distance'
        ? formatDistance(step.duration ?? 0)
        : 'until the lap button'
  const parts = [stepKindLabels[step.kind], duration]
  if (step.target) parts.push(targetText(step.target, sport))
  return parts.join(' · ')
}
