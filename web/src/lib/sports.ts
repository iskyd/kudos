import type { ActivitySport } from '@/api/activities'

export const sportLabels: Record<ActivitySport, string> = {
  running: 'Running',
  trail_running: 'Trail running',
  cycling: 'Cycling',
  pool_swimming: 'Pool swimming',
  open_water_swimming: 'Open water swimming',
  hiking: 'Hiking',
  strength: 'Strength',
  transition: 'Transition',
  other: 'Other',
}
