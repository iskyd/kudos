import { useState } from 'react'
import { Link } from 'react-router'
import type { ActivitySummary } from '@/api/activities'
import { type Calendar, useCalendar } from '@/api/analytics'
import type { HealthLogEntry, WellnessDay } from '@/api/wellness'
import { Empty, QueryStatus } from '@/components/card'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { addDays, calendarDays, formatDay, formatPeriod, toISODay } from '@/lib/format/date'
import { formatDistance, formatDuration, formatLocalClock, localDay } from '@/lib/format/units'
import { sportLabels } from '@/lib/sports'

type View = 'month' | 'week'

const weekdays = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
const healthLabels = { injury: 'Injury', illness: 'Illness', period: 'Period' } as const

export function CalendarPage() {
  const today = toISODay(new Date())
  const [view, setView] = useState<View>('month')
  const [anchor, setAnchor] = useState(today)
  const days = calendarDays(view, anchor)
  const from = days[0] ?? anchor
  const to = days.at(-1) ?? anchor
  const calendar = useCalendar(from, to)
  const month = anchor.slice(0, 7)
  const title = view === 'month' ? formatPeriod('month', `${month}-01`) : formatPeriod('week', from)

  // Moves the anchor by one view: a month from its first day, so 31 January
  // moves to February, not March.
  function step(n: number) {
    if (view === 'week') {
      setAnchor(addDays(anchor, 7 * n))
      return
    }
    const [y = 0, m = 1] = month.split('-').map(Number)
    setAnchor(toISODay(new Date(y, m - 1 + n, 1)))
  }

  const c = calendar.data
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <h1 className="text-2xl font-semibold">Calendar</h1>
        <div className="flex flex-wrap items-end gap-2">
          <div className="flex flex-col gap-2">
            <Label htmlFor="calendar-view">View</Label>
            <NativeSelect
              id="calendar-view"
              value={view}
              onChange={(event) => {
                setView(event.target.value as View)
              }}
            >
              <NativeSelectOption value="month">Month</NativeSelectOption>
              <NativeSelectOption value="week">Week</NativeSelectOption>
            </NativeSelect>
          </div>
          <Button
            variant="outline"
            onClick={() => {
              step(-1)
            }}
          >
            Previous
          </Button>
          <Button
            variant="outline"
            onClick={() => {
              setAnchor(today)
            }}
          >
            Today
          </Button>
          <Button
            variant="outline"
            onClick={() => {
              step(1)
            }}
          >
            Next
          </Button>
        </div>
      </div>
      <h2 className="text-lg font-semibold">{title}</h2>
      <QueryStatus query={calendar} />
      {c && c.activities.length + c.wellness.length + c.healthLog.length === 0 && (
        <Empty>Nothing recorded in this {view}.</Empty>
      )}
      <div>
        <div
          aria-hidden
          className="grid grid-cols-7 pb-1 text-center text-sm text-muted-foreground"
        >
          {weekdays.map((d) => (
            <span key={d}>{d}</span>
          ))}
        </div>
        <ol aria-label={title} className="grid grid-cols-7 gap-px rounded-md bg-border">
          {days.map((day) => (
            <DayCell
              key={day}
              day={day}
              muted={view === 'month' && !day.startsWith(month)}
              today={day === today}
              calendar={c}
              tall={view === 'week'}
            />
          ))}
        </ol>
      </div>
    </div>
  )
}

function DayCell({
  day,
  muted,
  today,
  calendar: c,
  tall,
}: {
  day: string
  muted: boolean
  today: boolean
  calendar: Calendar | undefined
  tall: boolean
}) {
  // Activities sit on the local day where they happened, whatever the viewer's timezone.
  const activities = c?.activities.filter((a) => localDay(a.startTime, a.utcOffsetS) === day) ?? []
  const checkIn = c?.wellness.find((w) => w.day === day && isCheckIn(w))
  const health =
    c?.healthLog.filter(
      (e) => e.startDate <= day && (e.endDate === undefined || e.endDate >= day),
    ) ?? []
  return (
    <li
      aria-label={formatDay(day)}
      aria-current={today ? 'date' : undefined}
      className={`flex min-w-0 flex-col gap-1 bg-background p-1 ${tall ? 'min-h-48' : 'min-h-24'} ${muted ? 'text-muted-foreground' : ''}`}
    >
      <span className={`self-end text-xs ${today ? 'font-semibold text-primary' : ''}`}>
        {Number(day.slice(8))}
      </span>
      {health.map((e) => (
        <HealthTag key={e.id} entry={e} />
      ))}
      {activities.map((a) => (
        <ActivityLink key={a.id} activity={a} />
      ))}
      {checkIn && <span className="text-xs text-muted-foreground">Check-in</span>}
    </li>
  )
}

// isCheckIn is true for a day with check-in scores or a note, not only body metrics.
function isCheckIn(w: WellnessDay): boolean {
  return [w.soreness, w.sleepQuality, w.stress, w.motivation, w.note].some((v) => v !== undefined)
}

function HealthTag({ entry: e }: { entry: HealthLogEntry }) {
  return (
    <span className="truncate rounded bg-destructive/15 px-1 text-xs text-destructive">
      {healthLabels[e.kind]}
      {e.description && `: ${e.description}`}
    </span>
  )
}

function ActivityLink({ activity: a }: { activity: ActivitySummary }) {
  const duration = a.timerTimeS ?? a.elapsedTimeS
  const details = [
    formatLocalClock(a.startTime, a.utcOffsetS).slice(0, 5),
    duration === undefined ? undefined : formatDuration(duration),
    a.distanceM ? formatDistance(a.distanceM) : undefined,
  ].filter(Boolean)
  return (
    <Link
      to={`/activities/${String(a.id)}`}
      className="flex min-w-0 flex-col rounded bg-muted px-1 text-xs hover:underline"
    >
      <span className="truncate font-medium">{sportLabels[a.sport]}</span>
      <span className="truncate text-muted-foreground">{details.join(' · ')}</span>
    </Link>
  )
}
