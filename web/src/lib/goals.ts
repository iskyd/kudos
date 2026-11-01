import type { Goal } from '@/api/goals'
import { parseDuration, parseKilometres, parseOptionalNumber } from '@/lib/format/input'
import { formatDistance, formatDuration, formatWhole } from '@/lib/format/units'

type Metric = Goal['metric']

export const metricLabels: Record<Metric, string> = {
  distance: 'Distance',
  duration: 'Time',
  ascent: 'Ascent',
}

// The target input's unit, as its label shows it.
export const targetUnits: Record<Metric, string> = {
  distance: 'km',
  duration: 'h:mm:ss or minutes',
  ascent: 'm',
}

export const statusLabels: Record<Goal['status'], string> = {
  upcoming: 'Not started',
  ahead: 'On track',
  behind: 'Behind',
  achieved: 'Achieved',
  missed: 'Missed',
}

// formatGoalValue renders a goal's value or target in the metric's display unit.
export function formatGoalValue(metric: Metric, value: number): string {
  if (metric === 'distance') return formatDistance(value)
  if (metric === 'duration') return formatDuration(value)
  return formatWhole(value, 'm')
}

// parseGoalTarget reads the target input into SI units: null when invalid or not positive.
export function parseGoalTarget(metric: Metric, text: string): number | null {
  const value =
    metric === 'distance'
      ? parseKilometres(text)
      : metric === 'duration'
        ? parseDuration(text)
        : parseOptionalNumber(text)
  return typeof value === 'number' && value > 0 ? value : null
}
