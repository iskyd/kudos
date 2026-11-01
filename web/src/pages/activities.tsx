import { useState, type ReactNode, type SubmitEvent } from 'react'
import { Link } from 'react-router'
import {
  type ActivityFilters,
  type ActivitySport,
  type ActivitySummary,
  useActivities,
  useCreateActivity,
  useUploadActivity,
  type Sport,
} from '@/api/activities'
import { useGear } from '@/api/gear'
import { problemMessage } from '@/api/problem'
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
import { parseDuration, parseKilometres } from '@/lib/format/input'
import { formatActivityStart, formatDistance, formatDuration } from '@/lib/format/units'
import { sportLabels } from '@/lib/sports'

// Sports a manual activity can have: every stored sport but multisport transitions.
const sports: { value: Sport; label: string }[] = (
  [
    'running',
    'trail_running',
    'cycling',
    'pool_swimming',
    'open_water_swimming',
    'hiking',
    'strength',
    'other',
  ] as const
).map((value) => ({ value, label: sportLabels[value] }))

const sortOptions: { value: ActivityFilters['sort']; label: string }[] = [
  { value: 'date', label: 'Newest' },
  { value: 'distance', label: 'Longest distance' },
  { value: 'duration', label: 'Longest duration' },
]

export function ActivitiesPage() {
  const [message, setMessage] = useState<string | null>(null)
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold">Activities</h1>
        <div className="flex gap-2">
          <UploadDialog onDone={setMessage} />
          <ManualActivityDialog onDone={setMessage} />
        </div>
      </div>
      {message && (
        <p role="status" className="text-sm text-muted-foreground">
          {message}
        </p>
      )}
      <ActivityList />
    </div>
  )
}

function ActivityList() {
  const [filters, setFilters] = useState<ActivityFilters>({ sort: 'date' })
  const activities = useActivities(filters)
  const gear = useGear().data?.pages[0]?.items ?? []
  const items = activities.data?.pages.flatMap((page) => page.items) ?? []
  const filtered = Boolean(
    filters.sport ?? filters.gear ?? filters.tag ?? filters.from ?? filters.to,
  )

  return (
    <section aria-label="Activity list" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor="filter-sport">Sport</Label>
          <NativeSelect
            id="filter-sport"
            value={filters.sport ?? ''}
            onChange={(event) => {
              const sport = event.target.value as ActivitySport | ''
              setFilters({ ...filters, sport: sport || undefined })
            }}
          >
            <NativeSelectOption value="">All sports</NativeSelectOption>
            {Object.entries(sportLabels).map(([value, label]) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        {gear.length > 0 && (
          <div className="flex flex-col gap-2">
            <Label htmlFor="filter-gear">Gear</Label>
            <NativeSelect
              id="filter-gear"
              value={filters.gear ?? ''}
              onChange={(event) => {
                const id = event.target.value
                setFilters({ ...filters, gear: id === '' ? undefined : Number(id) })
              }}
            >
              <NativeSelectOption value="">All gear</NativeSelectOption>
              {gear.map((g) => (
                <NativeSelectOption key={g.id} value={g.id}>
                  {g.name}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
        )}
        <div className="flex flex-col gap-2">
          <Label htmlFor="filter-tag">Pain tag</Label>
          <Input
            id="filter-tag"
            value={filters.tag ?? ''}
            onChange={(event) => {
              setFilters({ ...filters, tag: event.target.value.trim() || undefined })
            }}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="filter-from">From</Label>
          <Input
            id="filter-from"
            type="date"
            value={filters.from ?? ''}
            onChange={(event) => {
              setFilters({ ...filters, from: event.target.value || undefined })
            }}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="filter-to">To</Label>
          <Input
            id="filter-to"
            type="date"
            value={filters.to ?? ''}
            onChange={(event) => {
              setFilters({ ...filters, to: event.target.value || undefined })
            }}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="sort">Sort by</Label>
          <NativeSelect
            id="sort"
            value={filters.sort}
            onChange={(event) => {
              setFilters({ ...filters, sort: event.target.value as ActivityFilters['sort'] })
            }}
          >
            {sortOptions.map(({ value, label }) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
      </div>

      {activities.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
      {activities.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(activities.error)}
        </p>
      )}
      {activities.isSuccess && items.length === 0 && (
        <p className="text-sm text-muted-foreground">
          {filtered
            ? 'No activities match these filters.'
            : 'No activities yet. Upload a file, add one manually or connect Garmin in Settings.'}
        </p>
      )}
      {items.length > 0 && (
        <table className="w-full text-left text-sm">
          <thead className="text-muted-foreground">
            <tr>
              <th className="py-2 font-medium">Date</th>
              <th className="py-2 font-medium">Sport</th>
              <th className="py-2 text-right font-medium">Duration</th>
              <th className="py-2 text-right font-medium">Distance</th>
            </tr>
          </thead>
          <tbody>
            {items.map((a) => (
              <ActivityRow key={a.id} activity={a} />
            ))}
          </tbody>
        </table>
      )}
      {activities.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={activities.isFetchingNextPage}
          onClick={() => {
            void activities.fetchNextPage()
          }}
        >
          Show more
        </Button>
      )}
    </section>
  )
}

export function ActivityRow({ activity: a }: { activity: ActivitySummary }) {
  const duration = a.timerTimeS ?? a.elapsedTimeS
  return (
    <tr className="border-t border-border">
      <td className="py-2">
        <Link to={`/activities/${String(a.id)}`} className="underline">
          {formatActivityStart(a.startTime, a.utcOffsetS)}
        </Link>
      </td>
      <td className="py-2">{sportLabels[a.sport]}</td>
      <td className="py-2 text-right">{duration === undefined ? '—' : formatDuration(duration)}</td>
      <td className="py-2 text-right">
        {a.distanceM === undefined ? '—' : formatDistance(a.distanceM)}
      </td>
    </tr>
  )
}

function UploadDialog({ onDone }: { onDone: (message: string) => void }) {
  const [open, setOpen] = useState(false)
  const [missing, setMissing] = useState(false)
  const upload = useUploadActivity()

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    const input = event.currentTarget.elements.namedItem('file')
    const file = input instanceof HTMLInputElement ? input.files?.[0] : undefined
    if (!file) {
      setMissing(true)
      return
    }
    upload.mutate(file, {
      onSuccess: ({ activityIds }) => {
        setOpen(false)
        const n = activityIds.length
        onDone(`Imported ${String(n)} ${n === 1 ? 'activity' : 'activities'}.`)
      },
    })
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        upload.reset()
        setMissing(false)
      }}
    >
      <DialogTrigger asChild>
        <Button>Upload</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Upload an activity</DialogTitle>
          <DialogDescription>A FIT, GPX or TCX file up to 64 MB.</DialogDescription>
        </DialogHeader>
        <form noValidate onSubmit={onSubmit} className="flex flex-col gap-4">
          <Field name="file" label="File" error={missing ? 'Choose a file.' : undefined}>
            <Input
              id="file"
              name="file"
              type="file"
              accept=".fit,.gpx,.tcx"
              aria-invalid={missing}
            />
          </Field>
          {upload.isError && (
            <p role="alert" className="text-sm text-destructive">
              {problemMessage(upload.error)}
            </p>
          )}
          <Button type="submit" className="self-end" disabled={upload.isPending}>
            {upload.isPending ? 'Uploading…' : 'Upload'}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  )
}

type ManualErrors = Partial<Record<'start' | 'duration' | 'distance' | 'rpe', string>>

function ManualActivityDialog({ onDone }: { onDone: (message: string) => void }) {
  const [open, setOpen] = useState(false)
  const [errors, setErrors] = useState<ManualErrors>({})
  const create = useCreateActivity()

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const field = (name: string) => {
      const value = form.get(name)
      return typeof value === 'string' ? value.trim() : ''
    }
    const found: ManualErrors = {}
    const start = field('start')
    if (start === '') found.start = 'Enter the start date and time.'
    const duration = parseDuration(field('duration'))
    if (duration === null || duration <= 0) found.duration = 'Use h:mm:ss, mm:ss or minutes.'
    const distance = field('distance') === '' ? undefined : parseKilometres(field('distance'))
    if (distance === null) found.distance = 'Enter kilometres, e.g. 10.5.'
    const rpe = field('rpe') === '' ? undefined : Number(field('rpe'))
    if (rpe !== undefined && !(rpe >= 0 && rpe <= 10)) found.rpe = 'Use 0 to 10.'
    setErrors(found)
    if (Object.keys(found).length > 0 || duration === null || distance === null) return

    const notes = field('notes')
    create.mutate(
      {
        sport: field('sport') as Sport,
        // datetime-local gives YYYY-MM-DDTHH:MM, in the user's timezone as the API expects.
        localStartTime: start.slice(0, 16),
        durationS: duration,
        ...(distance !== undefined && { distanceM: distance }),
        ...(rpe !== undefined && { rpe }),
        ...(notes !== '' && { notes }),
      },
      {
        onSuccess: () => {
          setOpen(false)
          onDone('Activity saved.')
        },
      },
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        create.reset()
        setErrors({})
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline">Add manually</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add an activity</DialogTitle>
          <DialogDescription>For a session recorded without a device.</DialogDescription>
        </DialogHeader>
        <form noValidate onSubmit={onSubmit} className="flex flex-col gap-4">
          <Field name="sport" label="Sport">
            <NativeSelect id="sport" name="sport" defaultValue="running">
              {sports.map(({ value, label }) => (
                <NativeSelectOption key={value} value={value}>
                  {label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
          <Field name="start" label="Start" error={errors.start}>
            <Input
              id="start"
              name="start"
              type="datetime-local"
              aria-invalid={errors.start !== undefined}
            />
          </Field>
          <Field name="duration" label="Duration" error={errors.duration}>
            <Input
              id="duration"
              name="duration"
              placeholder="1:05:00"
              aria-invalid={errors.duration !== undefined}
            />
          </Field>
          <Field name="distance" label="Distance (km)" error={errors.distance}>
            <Input
              id="distance"
              name="distance"
              inputMode="decimal"
              aria-invalid={errors.distance !== undefined}
            />
          </Field>
          <Field name="rpe" label="RPE (0–10)" error={errors.rpe}>
            <Input
              id="rpe"
              name="rpe"
              type="number"
              min={0}
              max={10}
              step={0.5}
              aria-invalid={errors.rpe !== undefined}
            />
          </Field>
          <Field name="notes" label="Notes">
            <Textarea id="notes" name="notes" maxLength={2000} />
          </Field>
          {create.isError && (
            <p role="alert" className="text-sm text-destructive">
              {problemMessage(create.error)}
            </p>
          )}
          <Button type="submit" className="self-end" disabled={create.isPending}>
            Save
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function Field({
  name,
  label,
  error,
  children,
}: {
  name: string
  label: string
  error?: string | undefined
  children: ReactNode
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={name}>{label}</Label>
      {children}
      {error && <p className="text-sm text-destructive">{error}</p>}
    </div>
  )
}
