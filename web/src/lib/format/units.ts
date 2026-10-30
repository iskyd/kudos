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
