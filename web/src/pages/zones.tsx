import { useState, type SubmitEvent } from 'react'
import { Link } from 'react-router'
import { problemMessage } from '@/api/problem'
import {
  type ZoneSet,
  type ZoneSetInput,
  type ZoneSport,
  type ZoneSuggestion,
  type Zones,
  useAnswerSuggestion,
  useCreateZoneSet,
  useDeleteZoneSet,
  useZoneSets,
  useZoneSuggestions,
} from '@/api/zones'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { formatDay, toISODay } from '@/lib/format/date'
import { parseDuration, parseOptionalNumber } from '@/lib/format/input'
import { formatPace, speedFromPace } from '@/lib/format/units'
import { type ZoneMetric, zoneModelLabels, zoneRange } from '@/lib/format/zones'

const sportLabels: Record<ZoneSport, string> = {
  running: 'Running',
  cycling: 'Cycling',
  swimming: 'Swimming',
  general: 'Any sport',
}

// Which thresholds each sport takes; max and resting HR are open to all.
const fields: Record<ZoneSport, ('lthr' | 'pace' | 'ftp' | 'css')[]> = {
  running: ['lthr', 'pace'],
  cycling: ['ftp', 'lthr'],
  swimming: ['css'],
  general: [],
}

export function ZonesPage() {
  const zones = useZoneSets()
  const items = zones.data?.pages.flatMap((p) => p.items) ?? []
  return (
    <div className="flex max-w-2xl flex-col gap-8">
      <header className="flex flex-col gap-1">
        <Link to="/settings" className="text-sm text-muted-foreground underline">
          Settings
        </Link>
        <h1 className="text-2xl font-semibold">Training zones</h1>
        <p className="text-sm text-muted-foreground">
          Each version holds a sport&apos;s thresholds from a date on. An activity uses the version
          valid on its date.
        </p>
      </header>
      <Suggestions />
      <AddVersion />
      <section aria-label="Versions" className="flex flex-col gap-6">
        {zones.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
        {zones.isError && (
          <p role="alert" className="text-sm text-destructive">
            {problemMessage(zones.error)}
          </p>
        )}
        {zones.isSuccess && items.length === 0 && (
          <p className="text-sm text-muted-foreground">No zone versions yet.</p>
        )}
        {items.map((z) => (
          <Version key={z.id} z={z} />
        ))}
        {zones.hasNextPage && (
          <Button
            variant="outline"
            disabled={zones.isFetchingNextPage}
            onClick={() => {
              void zones.fetchNextPage()
            }}
          >
            Load more
          </Button>
        )}
      </section>
    </div>
  )
}

function AddVersion() {
  const create = useCreateZoneSet()
  const [sport, setSport] = useState<ZoneSport>('running')
  const [invalid, setInvalid] = useState<string | null>(null)
  // Bumped after a save to clear the inputs; a failed save keeps them.
  const [saved, setSaved] = useState(0)

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const field = (name: string) => {
      const v = form.get(name)
      return typeof v === 'string' ? v.trim() : ''
    }
    const body: ZoneSetInput = { sport, effectiveFrom: field('from') }
    const numbers = {
      lthrBpm: 'lthr',
      ftpW: 'ftp',
      maxHrBpm: 'max',
      restingHrBpm: 'resting',
    } as const
    for (const [key, name] of Object.entries(numbers)) {
      const v = parseOptionalNumber(field(name))
      if (v === null) {
        setInvalid('Heart rates and power are whole numbers.')
        return
      }
      if (v !== undefined) body[key as keyof typeof numbers] = v
    }
    for (const [key, name, per] of [
      ['thresholdSpeedMS', 'pace', 1000],
      ['cssMS', 'css', 100],
    ] as const) {
      if (field(name) === '') continue
      const seconds = parseDuration(field(name))
      const speed = seconds === null ? null : speedFromPace(seconds, per)
      if (speed === null) {
        setInvalid('Enter paces as m:ss.')
        return
      }
      body[key] = speed
    }
    setInvalid(null)
    create.mutate(body, {
      onSuccess: () => {
        setSaved((n) => n + 1)
      },
    })
  }

  const shown = fields[sport]
  return (
    <section aria-label="Add a version" className="flex flex-col gap-4">
      <h2 className="text-lg font-semibold">Add a version</h2>
      <form
        key={saved}
        aria-label="Add a version"
        className="grid grid-cols-1 gap-4 sm:grid-cols-2"
        onSubmit={onSubmit}
      >
        <Field id="zone-sport" label="Sport">
          <NativeSelect
            id="zone-sport"
            value={sport}
            onChange={(e) => {
              setSport(e.target.value as ZoneSport)
            }}
          >
            {Object.entries(sportLabels).map(([value, label]) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Field id="zone-from" label="Valid from">
          <Input
            id="zone-from"
            name="from"
            type="date"
            required
            defaultValue={toISODay(new Date())}
          />
        </Field>
        {shown.includes('lthr') && <NumberField name="lthr" label="Lactate threshold HR (bpm)" />}
        {shown.includes('pace') && (
          <NumberField name="pace" label="Threshold pace (/km)" placeholder="4:00" />
        )}
        {shown.includes('ftp') && <NumberField name="ftp" label="FTP (W)" />}
        {shown.includes('css') && (
          <NumberField name="css" label="CSS pace (/100 m)" placeholder="1:40" />
        )}
        <NumberField name="max" label="Max HR (bpm)" />
        <NumberField name="resting" label="Resting HR (bpm)" />
        <div className="flex flex-col gap-2 sm:col-span-2">
          {(invalid ?? create.isError) && (
            <p role="alert" className="text-sm text-destructive">
              {invalid ?? problemMessage(create.error)}
            </p>
          )}
          <Button type="submit" disabled={create.isPending} className="self-start">
            Add version
          </Button>
        </div>
      </form>
    </section>
  )
}

// Suggestions lists thresholds recorded files carried that differ from the
// version valid on their date and higher ones estimated from best efforts; only
// the user's confirmation creates a version.
function Suggestions() {
  const suggestions = useZoneSuggestions()
  const answer = useAnswerSuggestion()
  const items = suggestions.data?.pages.flatMap((p) => p.items) ?? []
  if (suggestions.isPending || (suggestions.isSuccess && items.length === 0)) return null
  return (
    <section aria-label="Suggestions" className="flex flex-col gap-4">
      <h2 className="text-lg font-semibold">Suggested thresholds</h2>
      {suggestions.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(suggestions.error)}
        </p>
      )}
      {items.map((s) => {
        const title = `${sportLabels[s.sport]} from ${formatDay(s.effectiveFrom)}`
        return (
          <article
            key={s.id}
            aria-label={title}
            className="flex flex-col gap-2 rounded-md border p-4"
          >
            <h3 className="font-medium">{title}</h3>
            <p className="text-sm">{suggestionText(s)}</p>
            {s.activityId !== undefined && (
              <Link to={`/activities/${String(s.activityId)}`} className="text-sm underline">
                From this activity
              </Link>
            )}
            <div className="flex gap-2">
              <Button
                size="sm"
                disabled={answer.isPending}
                onClick={() => {
                  answer.mutate({ id: s.id, confirm: true })
                }}
              >
                Confirm
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={answer.isPending}
                onClick={() => {
                  answer.mutate({ id: s.id, confirm: false })
                }}
              >
                Dismiss
              </Button>
            </div>
          </article>
        )
      })}
      {answer.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(answer.error)}
        </p>
      )}
      {suggestions.hasNextPage && (
        <Button
          variant="outline"
          disabled={suggestions.isFetchingNextPage}
          onClick={() => {
            void suggestions.fetchNextPage()
          }}
        >
          Load more suggestions
        </Button>
      )}
    </section>
  )
}

// suggestionText lists the suggested values with what the current version has.
function suggestionText(s: ZoneSuggestion): string {
  const bpm = (v: number) => `${String(v)} bpm`
  const parts: [string, number | undefined, number | undefined, (v: number) => string][] = [
    ['LTHR', s.lthrBpm, s.current?.lthrBpm, bpm],
    ['threshold pace', s.thresholdSpeedMS, s.current?.thresholdSpeedMS, (v) => formatPace(v)],
    ['FTP', s.ftpW, s.current?.ftpW, (v) => `${String(v)} W`],
    ['max HR', s.maxHrBpm, s.current?.maxHrBpm, bpm],
    ['resting HR', s.restingHrBpm, s.current?.restingHrBpm, bpm],
  ]
  return parts
    .flatMap(([label, v, was, format]) => {
      if (v === undefined) return []
      const now = `${label} ${format(v)}`
      return [was === undefined || was === v ? now : `${now} (was ${format(was)})`]
    })
    .join(', ')
}

function Version({ z }: { z: ZoneSet }) {
  const remove = useDeleteZoneSet()
  const title = `${sportLabels[z.sport]} from ${formatDay(z.effectiveFrom)}`
  const per = z.sport === 'swimming' ? 100 : 1000
  const thresholds = [
    z.lthrBpm !== undefined && `LTHR ${String(z.lthrBpm)} bpm`,
    z.thresholdSpeedMS !== undefined && `threshold pace ${formatPace(z.thresholdSpeedMS)}`,
    z.ftpW !== undefined && `FTP ${String(z.ftpW)} W`,
    z.cssMS !== undefined && `CSS ${formatPace(z.cssMS, 100)}`,
    z.maxHrBpm !== undefined && `max HR ${String(z.maxHrBpm)} bpm`,
    z.restingHrBpm !== undefined && `resting HR ${String(z.restingHrBpm)} bpm`,
  ].filter(Boolean)
  return (
    <article aria-label={title} className="flex flex-col gap-3 rounded-md border p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h3 className="font-medium">{title}</h3>
          <p className="text-sm text-muted-foreground">{thresholds.join(', ')}</p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={remove.isPending}
          onClick={() => {
            remove.mutate(z.id)
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
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <ZoneTable zones={z.zones} metric="hr" per={per} />
        <ZoneTable zones={z.zones} metric="pace" per={per} />
        <ZoneTable zones={z.zones} metric="power" per={per} />
      </div>
    </article>
  )
}

function ZoneTable({ zones, metric, per }: { zones: Zones; metric: ZoneMetric; per: number }) {
  const list = zones[metric]
  const model = zones[`${metric}Model`]
  if (!list || model === undefined) return null
  const label = zoneModelLabels[model] ?? model
  return (
    <table aria-label={label} className="text-sm">
      <caption className="text-left font-medium">{label}</caption>
      <tbody>
        {list.map((zone) => (
          <tr key={zone.name}>
            <th scope="row" className="pr-4 text-left font-normal text-muted-foreground">
              {zone.name}
            </th>
            <td>{zoneRange(metric, zone, per)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function Field({ id, label, children }: { id: string; label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
    </div>
  )
}

function NumberField({
  name,
  label,
  placeholder,
}: {
  name: string
  label: string
  placeholder?: string
}) {
  const id = `zone-${name}`
  return (
    <Field id={id} label={label}>
      <Input id={id} name={name} inputMode="decimal" placeholder={placeholder} />
    </Field>
  )
}
