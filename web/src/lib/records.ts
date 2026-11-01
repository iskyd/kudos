import type { RecordEffort } from '@/api/analytics'
import { formatDistance, formatDuration, formatWhole } from '@/lib/format/units'

export const effortLabels: Record<RecordEffort, string> = {
  fastest_1k: 'Fastest 1K',
  fastest_5k: 'Fastest 5K',
  fastest_10k: 'Fastest 10K',
  fastest_half_marathon: 'Fastest half marathon',
  fastest_marathon: 'Fastest marathon',
  fastest_swim_200m: 'Fastest 200 m swim',
  fastest_swim_400m: 'Fastest 400 m swim',
  longest_run: 'Longest run',
  longest_ride: 'Longest ride',
  longest_swim: 'Longest swim',
  longest_hike: 'Longest hike',
  power_5s: 'Best 5 s power',
  power_1m: 'Best 1 min power',
  power_5m: 'Best 5 min power',
  power_20m: 'Best 20 min power',
  power_60m: 'Best 60 min power',
}

// formatEffort renders a record's value: a time for fastest efforts, a
// distance for longest, watts for power.
export function formatEffort(effort: RecordEffort, value: number): string {
  if (effort.startsWith('fastest')) return formatDuration(value)
  if (effort.startsWith('longest')) return formatDistance(value)
  return formatWhole(value, 'W')
}
