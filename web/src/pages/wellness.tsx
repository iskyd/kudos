import { useState, type SubmitEvent } from 'react'
import { problemMessage } from '@/api/problem'
import {
  type HealthLogEntry,
  type HealthLogInput,
  type WellnessDay,
  type WellnessDayInput,
  useDeleteHealthLogEntry,
  useDeleteWellnessDay,
  useHealthLog,
  useSaveHealthLogEntry,
  useSaveWellnessDay,
  useWellnessDays,
} from '@/api/wellness'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { formatDay, toISODay } from '@/lib/format/date'
import { parseOptionalNumber } from '@/lib/format/input'
import { formatOneDecimal } from '@/lib/format/units'

type Score = 'soreness' | 'sleepQuality' | 'stress' | 'motivation'

const scores: { key: Score; label: string; scale: string }[] = [
  { key: 'soreness', label: 'Soreness', scale: '1 none – 5 severe' },
  { key: 'sleepQuality', label: 'Sleep quality', scale: '1 poor – 5 great' },
  { key: 'stress', label: 'Stress', scale: '1 low – 5 high' },
  { key: 'motivation', label: 'Motivation', scale: '1 low – 5 high' },
]

const metrics = [
  { key: 'weightKg', label: 'Weight (kg)', unit: 'kg' },
  { key: 'bodyFatPct', label: 'Body fat (%)', unit: '%' },
  { key: 'hydrationL', label: 'Hydration (L)', unit: 'L' },
] as const

const kindLabels: Record<HealthLogInput['kind'], string> = {
  injury: 'Injury',
  illness: 'Illness',
  period: 'Period',
}

// The cycle-tracking switch is a UI preference, so it lives in this browser only.
const cycleKey = 'kudos.cycleTracking'

function readCycleTracking(): boolean {
  try {
    return localStorage.getItem(cycleKey) === '1'
  } catch {
    return false
  }
}

function writeCycleTracking(on: boolean) {
  try {
    if (on) localStorage.setItem(cycleKey, '1')
    else localStorage.removeItem(cycleKey)
  } catch {
    // Storage unavailable: the switch lasts for this visit.
  }
}

export function WellnessPage() {
  return (
    <div className="flex flex-col gap-8">
      <h1 className="text-2xl font-semibold">Wellness</h1>
      <CheckIns />
      <HealthLog />
    </div>
  )
}

function CheckIns() {
  const days = useWellnessDays()
  const items = days.data?.pages.flatMap((page) => page.items) ?? []
  const [day, setDay] = useState(() => toISODay(new Date()))
  const entry = items.find((d) => d.day === day)

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-semibold">Daily check-in and body metrics</h2>
      <DayForm key={`${day}:${JSON.stringify(entry)}`} day={day} entry={entry} onDay={setDay} />
      {days.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
      {days.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(days.error)}
        </p>
      )}
      {days.isSuccess && items.length === 0 && (
        <p className="text-sm text-muted-foreground">No check-ins yet.</p>
      )}
      {items.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="text-muted-foreground">
              <tr>
                <th className="py-2 font-medium">Day</th>
                {scores.map((s) => (
                  <th key={s.key} className="py-2 text-right font-medium">
                    {s.label}
                  </th>
                ))}
                {metrics.map((m) => (
                  <th key={m.key} className="py-2 text-right font-medium">
                    {m.label.replace(/ \(.*\)/, '')}
                  </th>
                ))}
                <th className="py-2 font-medium">Note</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {items.map((d) => (
                <DayRow
                  key={d.day}
                  day={d}
                  onEdit={() => {
                    setDay(d.day)
                  }}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {days.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={days.isFetchingNextPage}
          onClick={() => {
            void days.fetchNextPage()
          }}
        >
          Show more days
        </Button>
      )}
    </section>
  )
}

function DayRow({ day: d, onEdit }: { day: WellnessDay; onEdit: () => void }) {
  const remove = useDeleteWellnessDay()
  const label = formatDay(d.day)
  return (
    <tr className="border-t border-border">
      <td className="py-2">{label}</td>
      {scores.map((s) => (
        <td key={s.key} className="py-2 text-right">
          {d[s.key] ?? '—'}
        </td>
      ))}
      {metrics.map((m) => {
        const v = d[m.key]
        return (
          <td key={m.key} className="py-2 text-right">
            {v === undefined ? '—' : formatOneDecimal(v, m.unit)}
          </td>
        )
      })}
      <td className="max-w-64 truncate py-2">{d.note}</td>
      <td className="py-2 text-right">
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" aria-label={`Edit ${label}`} onClick={onEdit}>
            Edit
          </Button>
          <Button
            variant="ghost"
            size="sm"
            aria-label={`Delete ${label}`}
            disabled={remove.isPending}
            onClick={() => {
              remove.mutate(d.day)
            }}
          >
            Delete
          </Button>
        </div>
        {remove.isError && (
          <p role="alert" className="text-sm text-destructive">
            {problemMessage(remove.error)}
          </p>
        )}
      </td>
    </tr>
  )
}

// DayForm edits one day, starting from what was entered for it.
function DayForm({
  day,
  entry,
  onDay,
}: {
  day: string
  entry?: WellnessDay
  onDay: (day: string) => void
}) {
  const save = useSaveWellnessDay()
  const [invalid, setInvalid] = useState<string | null>(null)

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const field = (name: string) => {
      const value = form.get(name)
      return typeof value === 'string' ? value.trim() : ''
    }
    const body: WellnessDayInput = {}
    for (const s of scores) {
      if (field(s.key) !== '') body[s.key] = Number(field(s.key))
    }
    for (const m of metrics) {
      const v = parseOptionalNumber(field(m.key))
      if (v === null) {
        setInvalid(`Enter ${m.label.toLowerCase()} as a number.`)
        return
      }
      if (v !== undefined) body[m.key] = v
    }
    if (field('note') !== '') body.note = field('note')
    if (Object.keys(body).length === 0) {
      setInvalid('Enter at least one value.')
      return
    }
    setInvalid(null)
    save.mutate({ day, body })
  }

  return (
    <form noValidate onSubmit={onSubmit} className="flex flex-col gap-4">
      <div className="flex flex-col gap-2 sm:w-48">
        <Label htmlFor="wellness-day">Day</Label>
        <Input
          id="wellness-day"
          type="date"
          value={day}
          onChange={(e) => {
            if (e.target.value !== '') onDay(e.target.value)
          }}
        />
      </div>
      <div className="grid gap-4 sm:grid-cols-4">
        {scores.map((s) => (
          <div key={s.key} className="flex flex-col gap-2">
            <Label htmlFor={`wellness-${s.key}`}>{s.label}</Label>
            <NativeSelect
              id={`wellness-${s.key}`}
              name={s.key}
              defaultValue={entry?.[s.key]?.toString() ?? ''}
              aria-describedby={`wellness-${s.key}-scale`}
            >
              <NativeSelectOption value="">—</NativeSelectOption>
              {[1, 2, 3, 4, 5].map((n) => (
                <NativeSelectOption key={n} value={String(n)}>
                  {n}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <span id={`wellness-${s.key}-scale`} className="text-xs text-muted-foreground">
              {s.scale}
            </span>
          </div>
        ))}
      </div>
      <div className="grid gap-4 sm:grid-cols-3">
        {metrics.map((m) => (
          <div key={m.key} className="flex flex-col gap-2">
            <Label htmlFor={`wellness-${m.key}`}>{m.label}</Label>
            <Input
              id={`wellness-${m.key}`}
              name={m.key}
              inputMode="decimal"
              defaultValue={entry?.[m.key]?.toString() ?? ''}
            />
          </div>
        ))}
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="wellness-note">Note</Label>
        <Textarea id="wellness-note" name="note" maxLength={2000} defaultValue={entry?.note} />
      </div>
      {(invalid ?? save.isError) && (
        <p role="alert" className="text-sm text-destructive">
          {invalid ?? problemMessage(save.error)}
        </p>
      )}
      {save.isSuccess && <p className="text-sm text-muted-foreground">Saved.</p>}
      <Button type="submit" className="self-start" disabled={save.isPending}>
        Save {formatDay(day)}
      </Button>
    </form>
  )
}

function HealthLog() {
  const log = useHealthLog()
  const items = log.data?.pages.flatMap((page) => page.items) ?? []
  const [cycle, setCycle] = useState(readCycleTracking)

  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h2 className="text-lg font-semibold">Injuries and illnesses</h2>
        <HealthLogDialog cycle={cycle} />
      </div>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={cycle}
          onChange={(e) => {
            setCycle(e.target.checked)
            writeCycleTracking(e.target.checked)
          }}
        />
        Track menstrual cycle
      </label>
      {log.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
      {log.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(log.error)}
        </p>
      )}
      {log.isSuccess && items.length === 0 && (
        <p className="text-sm text-muted-foreground">
          Nothing logged. Injuries and illnesses you log here are always shared with the coach.
        </p>
      )}
      {items.length > 0 && (
        <table className="w-full text-left text-sm">
          <thead className="text-muted-foreground">
            <tr>
              <th className="py-2 font-medium">Kind</th>
              <th className="py-2 font-medium">Description</th>
              <th className="py-2 font-medium">From</th>
              <th className="py-2 font-medium">To</th>
              <th className="py-2" />
            </tr>
          </thead>
          <tbody>
            {items.map((e) => (
              <HealthLogRow key={e.id} entry={e} cycle={cycle} />
            ))}
          </tbody>
        </table>
      )}
      {log.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={log.isFetchingNextPage}
          onClick={() => {
            void log.fetchNextPage()
          }}
        >
          Show more entries
        </Button>
      )}
    </section>
  )
}

function HealthLogRow({ entry: e, cycle }: { entry: HealthLogEntry; cycle: boolean }) {
  const remove = useDeleteHealthLogEntry()
  const label = `${kindLabels[e.kind]} from ${formatDay(e.startDate)}`
  return (
    <tr className="border-t border-border">
      <td className="py-2">{kindLabels[e.kind]}</td>
      <td className="py-2">{e.description}</td>
      <td className="py-2">{formatDay(e.startDate)}</td>
      <td className="py-2">{e.endDate ? formatDay(e.endDate) : 'Ongoing'}</td>
      <td className="py-2 text-right">
        <div className="flex justify-end gap-2">
          <HealthLogDialog entry={e} cycle={cycle} />
          <Button
            variant="ghost"
            size="sm"
            aria-label={`Delete ${label}`}
            disabled={remove.isPending}
            onClick={() => {
              remove.mutate(e.id)
            }}
          >
            Delete
          </Button>
        </div>
        {remove.isError && (
          <p role="alert" className="text-sm text-destructive">
            {problemMessage(remove.error)}
          </p>
        )}
      </td>
    </tr>
  )
}

// HealthLogDialog adds an entry, or edits one when given; periods are offered
// only while cycle tracking is on (or the entry is one).
function HealthLogDialog({ entry, cycle }: { entry?: HealthLogEntry; cycle: boolean }) {
  const [open, setOpen] = useState(false)
  const [invalid, setInvalid] = useState<string | null>(null)
  const save = useSaveHealthLogEntry()
  const kinds = Object.entries(kindLabels).filter(
    ([kind]) => kind !== 'period' || cycle || entry?.kind === 'period',
  )

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const field = (name: string) => {
      const value = form.get(name)
      return typeof value === 'string' ? value.trim() : ''
    }
    const kind = field('kind') as HealthLogInput['kind']
    const [description, startDate, endDate] = [field('description'), field('start'), field('end')]
    if (description === '' && kind !== 'period') {
      setInvalid('Describe the injury or illness.')
      return
    }
    if (startDate === '') {
      setInvalid('Enter the start date.')
      return
    }
    // ISO dates compare as strings.
    if (endDate !== '' && endDate < startDate) {
      setInvalid('The end date must not be before the start date.')
      return
    }
    setInvalid(null)
    save.mutate(
      {
        id: entry?.id,
        body: {
          kind,
          startDate,
          ...(description !== '' && { description }),
          ...(endDate !== '' && { endDate }),
        },
      },
      {
        onSuccess: () => {
          setOpen(false)
        },
      },
    )
  }

  const title = entry ? `Edit ${kindLabels[entry.kind].toLowerCase()}` : 'Log an entry'
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        save.reset()
        setInvalid(null)
      }}
    >
      <DialogTrigger asChild>
        {entry ? (
          <Button
            variant="ghost"
            size="sm"
            aria-label={`Edit ${kindLabels[entry.kind]} from ${formatDay(entry.startDate)}`}
          >
            Edit
          </Button>
        ) : (
          <Button>Log an entry</Button>
        )}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>Leave the end date empty while it is ongoing.</DialogDescription>
        </DialogHeader>
        <form noValidate onSubmit={onSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="log-kind">Kind</Label>
            <NativeSelect id="log-kind" name="kind" defaultValue={entry?.kind ?? 'injury'}>
              {kinds.map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="log-description">Description</Label>
            <Textarea
              id="log-description"
              name="description"
              maxLength={2000}
              defaultValue={entry?.description}
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="log-start">Start</Label>
              <Input id="log-start" name="start" type="date" defaultValue={entry?.startDate} />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="log-end">End (optional)</Label>
              <Input id="log-end" name="end" type="date" defaultValue={entry?.endDate} />
            </div>
          </div>
          {(invalid ?? save.isError) && (
            <p role="alert" className="text-sm text-destructive">
              {invalid ?? problemMessage(save.error)}
            </p>
          )}
          <Button type="submit" className="self-end" disabled={save.isPending}>
            Save
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  )
}
