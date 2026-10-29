// Calendar dates arrive as "YYYY-MM-DD" and carry no timezone: they are read and
// compared as local calendar days, never through UTC.

function parseDay(isoDate: string): Date {
  const [y = 0, m = 1, d = 1] = isoDate.split('-').map(Number)
  return new Date(y, m - 1, d)
}

// formatDay renders a calendar date as "28 October 2026".
export function formatDay(isoDate: string): string {
  return parseDay(isoDate).toLocaleDateString('en-GB', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  })
}

// formatDateTime renders an instant in the viewer's timezone as "29 October 2026 at 07:30".
export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString('en-GB', { dateStyle: 'long', timeStyle: 'short' })
}

// daysUntil counts calendar days from now's local date to isoDate (negative when past).
export function daysUntil(isoDate: string, now: Date): number {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  return Math.round((parseDay(isoDate).getTime() - today.getTime()) / 86_400_000)
}

// toISODay renders a local date as "YYYY-MM-DD", the form date inputs use.
export function toISODay(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${String(date.getFullYear())}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}
