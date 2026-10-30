// Display formatting for SI values from the API (metric only in v1).

// formatDuration renders seconds as "1:02:03", or "2:03" under an hour.
export function formatDuration(seconds: number): string {
  const total = Math.round(seconds)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${String(h)}:${pad(m)}:${pad(s)}` : `${String(m)}:${pad(s)}`
}

// formatDistance renders metres as kilometres with two decimals: "10.02 km".
export function formatDistance(metres: number): string {
  return `${(metres / 1000).toFixed(2)} km`
}

// formatActivityStart renders a start in the local time where the activity
// happened ("29 Oct 2026, 07:30"), whatever the viewer's timezone.
export function formatActivityStart(startTime: string, utcOffsetS: number): string {
  const local = new Date(new Date(startTime).getTime() + utcOffsetS * 1000)
  return local.toLocaleString('en-GB', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    timeZone: 'UTC',
  })
}

// formatLocalClock renders an instant as the local clock time where the activity
// happened ("07:30:05").
export function formatLocalClock(time: string, utcOffsetS: number): string {
  return new Date(new Date(time).getTime() + utcOffsetS * 1000).toISOString().slice(11, 19)
}

// localDay is the local calendar day where the activity happened ("2026-10-10").
export function localDay(time: string, utcOffsetS: number): string {
  return new Date(new Date(time).getTime() + utcOffsetS * 1000).toISOString().slice(0, 10)
}

// paceSeconds turns a speed into seconds per `per` metres (1000 for /km, 100 for
// swimming); null when standing still.
export function paceSeconds(metresPerSecond: number, per = 1000): number | null {
  return metresPerSecond > 0 ? per / metresPerSecond : null
}

// speedFromPace turns a pace (seconds per `per` metres) back into metres per second.
export function speedFromPace(seconds: number, per = 1000): number | null {
  return seconds > 0 ? per / seconds : null
}

// formatPace renders a speed as a pace: "4:35 /km", or "1:45 /100 m" with per = 100.
export function formatPace(metresPerSecond: number, per = 1000): string {
  const s = paceSeconds(metresPerSecond, per)
  if (s === null) return '—'
  return `${formatDuration(s)} ${per === 1000 ? '/km' : `/${String(per)} m`}`
}

// formatSpeed renders metres per second as "28.4 km/h".
export function formatSpeed(metresPerSecond: number): string {
  return `${(metresPerSecond * 3.6).toFixed(1)} km/h`
}

// formatWhole renders a value rounded to a whole number with its unit: "152 bpm".
export function formatWhole(value: number, unit: string): string {
  return `${String(Math.round(value))} ${unit}`
}
