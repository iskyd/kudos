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

// addDays returns the calendar date n days after isoDate (before when negative).
export function addDays(isoDate: string, n: number): string {
  const d = parseDay(isoDate)
  d.setDate(d.getDate() + n)
  return toISODay(d)
}

// mondayOf returns the Monday starting isoDate's week, as the API's weeks do.
export function mondayOf(isoDate: string): string {
  return addDays(isoDate, -((parseDay(isoDate).getDay() + 6) % 7))
}

// periodStarts returns the first days of the n weeks (from Monday), months or
// years up to the one holding isoDate, newest first, as the API's periods start.
export function periodStarts(
  period: 'week' | 'month' | 'year',
  isoDate: string,
  n: number,
): string[] {
  const d = parseDay(isoDate)
  return Array.from({ length: n }, (_, i) => {
    switch (period) {
      case 'week':
        return addDays(mondayOf(isoDate), -7 * i)
      case 'month':
        return toISODay(new Date(d.getFullYear(), d.getMonth() - i, 1))
      default:
        return toISODay(new Date(d.getFullYear() - i, 0, 1))
    }
  })
}

// calendarDays returns the days a calendar shows for isoDate: its week (Monday
// to Sunday), or its month padded to whole weeks.
export function calendarDays(view: 'week' | 'month', isoDate: string): string[] {
  let first = mondayOf(isoDate)
  let last = addDays(first, 6)
  if (view === 'month') {
    const d = parseDay(isoDate)
    first = mondayOf(toISODay(new Date(d.getFullYear(), d.getMonth(), 1)))
    last = addDays(mondayOf(toISODay(new Date(d.getFullYear(), d.getMonth() + 1, 0))), 6)
  }
  const days = [first]
  while (days.at(-1) !== last) days.push(addDays(days.at(-1) ?? last, 1))
  return days
}

// formatPeriod names a period by its first day: "Week of 26 October 2026",
// "October 2026" or "2026".
export function formatPeriod(period: 'week' | 'month' | 'year', start: string): string {
  if (period === 'week') return `Week of ${formatDay(start)}`
  if (period === 'year') return start.slice(0, 4)
  return parseDay(start).toLocaleDateString('en-GB', { month: 'long', year: 'numeric' })
}

// toISODay renders a local date as "YYYY-MM-DD", the form date inputs use.
export function toISODay(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${String(date.getFullYear())}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}
