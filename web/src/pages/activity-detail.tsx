import { useMemo, useState, type ReactNode, type SubmitEvent } from 'react'
import { Link, useParams } from 'react-router'
import {
  type ActivityDetail,
  type ActivitySport,
  type ActivityStreams,
  type Lap,
  useActivity,
  useActivityStreams,
  useConfig,
  useLaps,
  useSetAnnotation,
  useStrengthSets,
  useSwimLengths,
} from '@/api/activities'
import { ApiError, problemMessage } from '@/api/problem'
import { RouteMap } from '@/components/route-map'
import { StreamChart } from '@/components/stream-chart'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { parseOptionalNumber, parseTags } from '@/lib/format/input'
import {
  formatActivityStart,
  formatDistance,
  formatDuration,
  formatPace,
  formatSpeed,
  formatWhole,
  paceSeconds,
} from '@/lib/format/units'
import { sportLabels } from '@/lib/sports'

// Pace is shown per km on foot and per 100 m in the water; other sports show speed.
function pacePer(sport: ActivitySport): number | null {
  switch (sport) {
    case 'running':
    case 'trail_running':
    case 'hiking':
      return 1000
    case 'pool_swimming':
    case 'open_water_swimming':
      return 100
    default:
      return null
  }
}

function speedText(sport: ActivitySport, speed: number | undefined): string {
  if (speed === undefined) return '—'
  const per = pacePer(sport)
  return per === null ? formatSpeed(speed) : formatPace(speed, per)
}

const dash = (v: number | undefined, f: (v: number) => string) => (v === undefined ? '—' : f(v))

export function ActivityDetailPage() {
  const id = Number(useParams().id)
  const activity = useActivity(id)

  if (activity.isPending) return <p className="text-sm text-muted-foreground">Loading…</p>
  if (activity.isError) {
    const notFound = activity.error instanceof ApiError && activity.error.problem.status === 404
    return (
      <p role="alert" className="text-sm text-destructive">
        {notFound ? 'Activity not found.' : problemMessage(activity.error)}
      </p>
    )
  }
  const a = activity.data
  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-1">
        <Link to="/activities" className="text-sm text-muted-foreground underline">
          Activities
        </Link>
        <h1 className="text-2xl font-semibold">{sportLabels[a.sport]}</h1>
        <p className="text-sm text-muted-foreground">
          {formatActivityStart(a.startTime, a.utcOffsetS)}
        </p>
      </header>
      <Summary a={a} />
      <Annotation key={a.id} a={a} />
      {a.legs.length > 0 && <Legs a={a} />}
      <Streams a={a} />
      <Laps a={a} />
      {a.sport === 'pool_swimming' && <Lengths id={a.id} />}
      {a.sport === 'strength' && <Sets id={a.id} />}
    </div>
  )
}

function Summary({ a }: { a: ActivityDetail }) {
  const duration = a.timerTimeS ?? a.elapsedTimeS
  const stats: [string, string][] = [
    ['Distance', dash(a.distanceM, formatDistance)],
    ['Duration', dash(duration, formatDuration)],
    [pacePer(a.sport) === null ? 'Avg speed' : 'Avg pace', speedText(a.sport, a.avgSpeedMS)],
    ['Avg heart rate', dash(a.avgHrBpm, (v) => formatWhole(v, 'bpm'))],
    ['Avg power', dash(a.avgPowerW, (v) => formatWhole(v, 'W'))],
    ['Ascent', dash(a.ascentM, (v) => formatWhole(v, 'm'))],
    ['Calories', dash(a.caloriesKcal, (v) => formatWhole(v, 'kcal'))],
  ]
  return (
    <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
      {stats.map(([label, value]) => (
        <div key={label} className="flex flex-col">
          <dt className="text-sm text-muted-foreground">{label}</dt>
          <dd className="text-lg font-medium">{value}</dd>
        </div>
      ))}
    </dl>
  )
}

function Legs({ a }: { a: ActivityDetail }) {
  return (
    <Section title="Legs">
      <ol className="flex flex-col gap-1 text-sm">
        {a.legs.map((leg) => (
          <li key={leg.id}>
            {leg.id === a.id ? (
              <span aria-current="page" className="font-medium">
                {sportLabels[leg.sport]}
              </span>
            ) : (
              <Link to={`/activities/${String(leg.id)}`} className="underline">
                {sportLabels[leg.sport]}
              </Link>
            )}
            <span className="text-muted-foreground">
              {' '}
              · {dash(leg.timerTimeS ?? leg.elapsedTimeS, formatDuration)} ·{' '}
              {dash(leg.distanceM, formatDistance)}
            </span>
          </li>
        ))}
      </ol>
    </Section>
  )
}

// Paces slower than this (stops, standing at lights) would flatten the pace chart.
const slowestChartPace = 30 * 60

function Streams({ a }: { a: ActivityDetail }) {
  const streams = useActivityStreams(a.id)
  const config = useConfig()
  const [hover, setHover] = useState<number | null>(null)
  // Stable across hovers: new values or formatters would rebuild every chart,
  // which resets the shared cursor and loses the hover.
  const series = useMemo(
    () => (streams.data ? charts(a.sport, streams.data) : []),
    [a.sport, streams.data],
  )

  if (streams.isPending) return <p className="text-sm text-muted-foreground">Loading charts…</p>
  if (streams.isError) {
    return (
      <p role="alert" className="text-sm text-destructive">
        {problemMessage(streams.error)}
      </p>
    )
  }
  const s = streams.data
  if (s.timeS.length === 0) {
    return <p className="text-sm text-muted-foreground">This activity has no recorded samples.</p>
  }
  // Virtual activities are mapped onto fictional places: never a map (invariant 18).
  const lat = a.subtype === 'virtual' ? undefined : s.lat
  return (
    <div className="flex flex-col gap-4">
      {lat && s.lon && config.data && (
        <RouteMap tileUrl={config.data.mapTileUrl} lat={lat} lon={s.lon} hoverIndex={hover} />
      )}
      {series.map((c) => (
        <StreamChart key={c.label} {...c} time={s.timeS} onHover={setHover} />
      ))}
    </div>
  )
}

function charts(sport: ActivitySport, s: ActivityStreams) {
  const out: {
    label: string
    values: (number | null)[]
    format: (v: number) => string
    invert?: boolean
  }[] = []
  if (s.hrBpm)
    out.push({ label: 'Heart rate', values: s.hrBpm, format: (v) => formatWhole(v, 'bpm') })
  const per = pacePer(sport)
  if (s.speedMS && per === null) {
    out.push({ label: 'Speed', values: s.speedMS, format: formatSpeed })
  } else if (s.speedMS && per !== null) {
    const values = s.speedMS.map((v) => {
      const p = v === null ? null : paceSeconds(v, per)
      return p === null || p > slowestChartPace ? null : p
    })
    out.push({ label: 'Pace', values, format: formatDuration, invert: true })
  }
  if (s.powerW) out.push({ label: 'Power', values: s.powerW, format: (v) => formatWhole(v, 'W') })
  if (s.cadence) {
    const unit = sport === 'cycling' ? 'rpm' : 'spm'
    out.push({ label: 'Cadence', values: s.cadence, format: (v) => formatWhole(v, unit) })
  }
  if (s.altitudeM) {
    out.push({ label: 'Elevation', values: s.altitudeM, format: (v) => formatWhole(v, 'm') })
  }
  return out
}

function Laps({ a }: { a: ActivityDetail }) {
  const laps = useLaps(a.id)
  const steps = a.workout?.steps ?? []
  const stepName = (l: Lap) =>
    steps.find((st) => st.index === l.workoutStepIndex)?.name ?? l.intensity ?? ''
  return (
    <ChildTable
      title="Laps"
      query={laps}
      empty="No laps recorded."
      head={[
        '#',
        'Step',
        'Duration',
        'Distance',
        pacePer(a.sport) === null ? 'Speed' : 'Pace',
        'Avg HR',
        'Avg power',
      ]}
      row={(l) => [
        String(l.index + 1),
        stepName(l),
        dash(l.timerTimeS ?? l.elapsedTimeS, formatDuration),
        dash(l.distanceM, formatDistance),
        speedText(a.sport, l.avgSpeedMS),
        dash(l.avgHrBpm, (v) => formatWhole(v, 'bpm')),
        dash(l.avgPowerW, (v) => formatWhole(v, 'W')),
      ]}
    />
  )
}

function Lengths({ id }: { id: number }) {
  const lengths = useSwimLengths(id, true)
  return (
    <ChildTable
      title="Lengths"
      query={lengths}
      empty="No lengths recorded."
      head={['#', 'Stroke', 'Time', 'Strokes', 'SWOLF', 'Pace']}
      row={(l) =>
        l.active
          ? [
              String(l.index + 1),
              l.stroke ?? '',
              dash(l.timerTimeS, formatDuration),
              dash(l.strokes, String),
              dash(l.swolf, (v) => String(Math.round(v))),
              dash(l.avgSpeedMS, (v) => formatPace(v, 100)),
            ]
          : [String(l.index + 1), 'Rest', dash(l.timerTimeS, formatDuration), '', '', '']
      }
    />
  )
}

function Sets({ id }: { id: number }) {
  const sets = useStrengthSets(id, true)
  return (
    <ChildTable
      title="Sets"
      query={sets}
      empty="No sets recorded."
      head={['#', 'Exercise', 'Reps', 'Weight', 'Duration']}
      row={(st) =>
        st.active
          ? [
              String(st.index + 1),
              st.exerciseName ?? '',
              dash(st.reps, String),
              st.weightKg ? formatWhole(st.weightKg, 'kg') : '—',
              dash(st.durationS, formatDuration),
            ]
          : [String(st.index + 1), 'Rest', '', '', dash(st.durationS, formatDuration)]
      }
    />
  )
}

interface ChildQuery<T> {
  isPending: boolean
  isError: boolean
  error: unknown
  data?: { pages: { items: T[] }[] }
  hasNextPage: boolean
  isFetchingNextPage: boolean
  fetchNextPage: () => Promise<unknown>
}

function ChildTable<T>({
  title,
  query,
  empty,
  head,
  row,
}: {
  title: string
  query: ChildQuery<T>
  empty: string
  head: string[]
  row: (item: T) => string[]
}) {
  const items = query.data?.pages.flatMap((p) => p.items) ?? []
  return (
    <Section title={title}>
      {query.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
      {query.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(query.error)}
        </p>
      )}
      {!query.isPending && !query.isError && items.length === 0 && (
        <p className="text-sm text-muted-foreground">{empty}</p>
      )}
      {items.length > 0 && (
        <table aria-label={title} className="w-full text-left text-sm">
          <thead className="text-muted-foreground">
            <tr>
              {head.map((h) => (
                <th key={h} className="py-1 pr-4 font-medium">
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {items.map((item, i) => (
              <tr key={i} className="border-t border-border">
                {row(item).map((cell, j) => (
                  <td key={j} className="py-1 pr-4">
                    {cell}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {query.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={query.isFetchingNextPage}
          onClick={() => {
            void query.fetchNextPage()
          }}
        >
          Show more
        </Button>
      )}
    </Section>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold">{title}</h2>
      {children}
    </section>
  )
}

// Garmin's five feel levels, kept as it records them (0–100).
const moods: { value: number; label: string }[] = [
  { value: 0, label: 'Very weak' },
  { value: 25, label: 'Weak' },
  { value: 50, label: 'Normal' },
  { value: 75, label: 'Strong' },
  { value: 100, label: 'Very strong' },
]

const conditions = ['sun', 'cloud', 'rain', 'wind', 'snow'] as const
const conditionLabels: Record<(typeof conditions)[number], string> = {
  sun: 'Sun',
  cloud: 'Cloud',
  rain: 'Rain',
  wind: 'Wind',
  snow: 'Snow',
}

const numberText = (v: number | undefined) => (v === undefined ? '' : String(v))

// Annotation is the user's own record of the activity: RPE and mood (pre-filled
// from the watch when recorded), notes, pain tags and weather.
function Annotation({ a }: { a: ActivityDetail }) {
  const save = useSetAnnotation(a.id)
  const [rpe, setRpe] = useState(numberText(a.rpe))
  const [feel, setFeel] = useState(numberText(a.feel))
  const [notes, setNotes] = useState(a.notes ?? '')
  const [tags, setTags] = useState(a.painTags.join(', '))
  const [temperature, setTemperature] = useState(numberText(a.weatherTemperatureC))
  const [condition, setCondition] = useState<string>(a.weatherCondition ?? '')
  const [invalid, setInvalid] = useState<string | null>(null)
  const moodOptions =
    moods.some((m) => String(m.value) === feel) || feel === ''
      ? moods
      : [...moods, { value: Number(feel), label: feel }]

  function changed<T>(set: (v: T) => void) {
    return (v: T) => {
      set(v)
      save.reset()
      setInvalid(null)
    }
  }

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    const rpeValue = parseOptionalNumber(rpe)
    const tempValue = parseOptionalNumber(temperature)
    if (rpeValue === null || (rpeValue !== undefined && (rpeValue < 0 || rpeValue > 10))) {
      setInvalid('RPE must be a number from 0 to 10.')
      return
    }
    if (tempValue === null) {
      setInvalid('Temperature must be a number.')
      return
    }
    save.mutate({
      rpe: rpeValue,
      feel: feel === '' ? undefined : Number(feel),
      notes: notes.trim() === '' ? undefined : notes,
      painTags: parseTags(tags),
      weatherTemperatureC: tempValue,
      weatherCondition: condition === '' ? undefined : (condition as (typeof conditions)[number]),
    })
  }

  return (
    <Section title="Notes and feel">
      <form onSubmit={onSubmit} className="grid gap-4 sm:grid-cols-2">
        <Field id="rpe" label="RPE (0–10)">
          <Input
            id="rpe"
            inputMode="decimal"
            value={rpe}
            onChange={(e) => {
              changed(setRpe)(e.target.value)
            }}
          />
        </Field>
        <Field id="feel" label="Mood">
          <NativeSelect
            id="feel"
            value={feel}
            onChange={(e) => {
              changed(setFeel)(e.target.value)
            }}
          >
            <NativeSelectOption value="">Not set</NativeSelectOption>
            {moodOptions.map((m) => (
              <NativeSelectOption key={m.value} value={String(m.value)}>
                {m.label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Field id="pain-tags" label="Pain tags (comma-separated)">
          <Input
            id="pain-tags"
            value={tags}
            onChange={(e) => {
              changed(setTags)(e.target.value)
            }}
          />
        </Field>
        <div className="grid grid-cols-2 gap-4">
          <Field id="weather-temperature" label="Temperature (°C)">
            <Input
              id="weather-temperature"
              inputMode="decimal"
              value={temperature}
              onChange={(e) => {
                changed(setTemperature)(e.target.value)
              }}
            />
          </Field>
          <Field id="weather-condition" label="Weather">
            <NativeSelect
              id="weather-condition"
              value={condition}
              onChange={(e) => {
                changed(setCondition)(e.target.value)
              }}
            >
              <NativeSelectOption value="">Not set</NativeSelectOption>
              {conditions.map((c) => (
                <NativeSelectOption key={c} value={c}>
                  {conditionLabels[c]}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
        </div>
        <div className="sm:col-span-2">
          <Field id="notes" label="Notes">
            <Textarea
              id="notes"
              value={notes}
              onChange={(e) => {
                changed(setNotes)(e.target.value)
              }}
            />
          </Field>
        </div>
        <div className="flex items-center gap-4 sm:col-span-2">
          <Button type="submit" disabled={save.isPending}>
            Save
          </Button>
          {(invalid ?? save.isError) && (
            <p role="alert" className="text-sm text-destructive">
              {invalid ?? problemMessage(save.error)}
            </p>
          )}
          {save.isSuccess && (
            <p role="status" className="text-sm text-muted-foreground">
              Saved.
            </p>
          )}
        </div>
      </form>
    </Section>
  )
}

function Field({ id, label, children }: { id: string; label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
    </div>
  )
}
