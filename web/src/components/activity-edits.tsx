import { useState, type ReactNode, type SubmitEvent } from 'react'
import { Link } from 'react-router'
import {
  type ActivityDetail,
  type ActivityEdit,
  type ActivityEditRequest,
  type ActivitySport,
  useActivities,
  useAddEdit,
  useUndoEdit,
} from '@/api/activities'
import { problemMessage } from '@/api/problem'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { parseDuration } from '@/lib/format/input'
import { formatActivityStart, formatLocalClock, localDay } from '@/lib/format/units'
import { sportLabels } from '@/lib/sports'

type Subtype = NonNullable<ActivityEditRequest['subtype']>
const subtypeLabels: Record<Subtype, string> = {
  outdoor: 'Outdoor',
  indoor: 'Indoor',
  virtual: 'Virtual',
  treadmill: 'Treadmill',
}

// ActivityEdits lists an activity's edits with undo, and applies new ones.
// Edits never change the recorded samples.
export function ActivityEdits({ a }: { a: ActivityDetail }) {
  if (a.mergedInto !== undefined) {
    return (
      <p role="note" className="text-sm text-muted-foreground">
        This activity was merged into{' '}
        <Link to={`/activities/${String(a.mergedInto)}`} className="underline">
          another activity
        </Link>
        ; edit it there.
      </p>
    )
  }
  return (
    <section aria-label="Edits" className="flex flex-col gap-4">
      <h2 className="text-lg font-semibold">Edits</h2>
      {a.splitOf !== undefined && (
        <p className="text-sm text-muted-foreground">
          Second part of{' '}
          <Link to={`/activities/${String(a.splitOf)}`} className="underline">
            a split activity
          </Link>
          .
        </p>
      )}
      <EditList a={a} />
      <EditForms key={a.edits.length} a={a} />
    </section>
  )
}

function EditList({ a }: { a: ActivityDetail }) {
  const undo = useUndoEdit(a.id)
  if (a.edits.length === 0) {
    return <p className="text-sm text-muted-foreground">No edits: everything is as recorded.</p>
  }
  return (
    <div className="flex flex-col gap-2">
      <ul className="flex flex-col gap-2 text-sm">
        {a.edits.map((e) => (
          <li key={e.id} className="flex items-center justify-between gap-4">
            <span>{describe(e, a.utcOffsetS)}</span>
            <Button
              variant="outline"
              size="sm"
              disabled={undo.isPending}
              onClick={() => {
                undo.mutate(e.id)
              }}
            >
              Undo
            </Button>
          </li>
        ))}
      </ul>
      {undo.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(undo.error)}
        </p>
      )}
    </div>
  )
}

function describe(e: ActivityEdit, offset: number): ReactNode {
  const clock = (t?: string) => (t === undefined ? '?' : formatLocalClock(t, offset))
  const other = (label: string) =>
    e.activityId === undefined ? (
      label
    ) : (
      <Link to={`/activities/${String(e.activityId)}`} className="underline">
        {label}
      </Link>
    )
  switch (e.kind) {
    case 'sport':
      return `Sport changed to ${e.sport ? sportLabels[e.sport] : '?'} (${e.subtype ? subtypeLabels[e.subtype].toLowerCase() : '?'})`
    case 'trim':
      return `Trimmed to ${clock(e.from)}–${clock(e.to)}`
    case 'exclude_hr':
      return `Heart rate excluded ${clock(e.from)}–${clock(e.to)}`
    case 'split':
      return (
        <>
          Split at {clock(e.at)}; {other('second part')}
        </>
      )
    case 'merge':
      return <>Merged with {other('another activity')}</>
  }
}

// Offsets are entered from the activity's current start, as h:mm:ss, mm:ss or minutes.
const offsetHint = 'Use h:mm:ss, mm:ss or minutes from the start.'

function EditForms({ a }: { a: ActivityDetail }) {
  const add = useAddEdit(a.id)
  const [invalid, setInvalid] = useState<string | null>(null)
  const day = localDay(a.startTime, a.utcOffsetS)
  const sameDay = useActivities({ sort: 'date', from: day, to: day })
  const candidates = (sameDay.data?.pages[0]?.items ?? []).filter((o) => o.id !== a.id)
  const joined =
    a.splitOf !== undefined || a.edits.some((e) => e.kind === 'merge' || e.kind === 'split')

  function submit(
    event: SubmitEvent<HTMLFormElement>,
    build: (f: (n: string) => string) => ActivityEditRequest | string,
  ) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const field = (name: string) => {
      const v = form.get(name)
      return typeof v === 'string' ? v.trim() : ''
    }
    const body = build(field)
    if (typeof body === 'string') {
      setInvalid(body)
      return
    }
    setInvalid(null)
    add.mutate(body)
  }

  const range =
    (kind: 'trim' | 'exclude_hr') =>
    (field: (n: string) => string): ActivityEditRequest | string => {
      const fromS = parseDuration(field(`${kind}-from`))
      const toS = parseDuration(field(`${kind}-to`))
      if (fromS === null || toS === null) return offsetHint
      if (toS <= fromS) return 'The end must come after the start.'
      return { kind, fromS, toS }
    }

  return (
    <div className="flex flex-col gap-4">
      <form
        aria-label="Change sport"
        className="flex flex-wrap items-end gap-2"
        onSubmit={(e) => {
          submit(e, (field) => ({
            kind: 'sport',
            sport: field('sport') as ActivitySport,
            subtype: field('subtype') as Subtype,
          }))
        }}
      >
        <Field id="edit-sport" label="Sport">
          <NativeSelect id="edit-sport" name="sport" defaultValue={a.sport}>
            {Object.entries(sportLabels).map(([value, label]) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Field id="edit-subtype" label="Type">
          <NativeSelect id="edit-subtype" name="subtype" defaultValue={a.subtype}>
            {Object.entries(subtypeLabels).map(([value, label]) => (
              <NativeSelectOption key={value} value={value}>
                {label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Button type="submit" variant="outline" disabled={add.isPending}>
          Change sport
        </Button>
      </form>

      {(['trim', 'exclude_hr'] as const).map((kind) => (
        <form
          key={kind}
          aria-label={kind === 'trim' ? 'Trim' : 'Exclude heart rate'}
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            submit(e, range(kind))
          }}
        >
          <Field
            id={`${kind}-from`}
            label={kind === 'trim' ? 'Keep from' : 'Exclude heart rate from'}
          >
            <Input id={`${kind}-from`} name={`${kind}-from`} placeholder="0:05:00" />
          </Field>
          <Field id={`${kind}-to`} label="To">
            <Input id={`${kind}-to`} name={`${kind}-to`} placeholder="1:00:00" />
          </Field>
          <Button type="submit" variant="outline" disabled={add.isPending}>
            {kind === 'trim' ? 'Trim' : 'Exclude'}
          </Button>
        </form>
      ))}

      {!joined && (
        <form
          aria-label="Split"
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            submit(e, (field) => {
              const atS = parseDuration(field('split-at'))
              return atS === null || atS <= 0 ? offsetHint : { kind: 'split', atS }
            })
          }}
        >
          <Field id="split-at" label="Split at">
            <Input id="split-at" name="split-at" placeholder="0:30:00" />
          </Field>
          <Button type="submit" variant="outline" disabled={add.isPending}>
            Split
          </Button>
        </form>
      )}

      {!joined && candidates.length > 0 && (
        <form
          aria-label="Merge"
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            submit(e, (field) => ({ kind: 'merge', activityId: Number(field('merge-with')) }))
          }}
        >
          <Field id="merge-with" label="Merge with">
            <NativeSelect id="merge-with" name="merge-with">
              {candidates.map((o) => (
                <NativeSelectOption key={o.id} value={o.id}>
                  {`${sportLabels[o.sport]}, ${formatActivityStart(o.startTime, o.utcOffsetS)}`}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
          <Button type="submit" variant="outline" disabled={add.isPending}>
            Merge
          </Button>
        </form>
      )}

      {(invalid ?? add.isError) && (
        <p role="alert" className="text-sm text-destructive">
          {invalid ?? problemMessage(add.error)}
        </p>
      )}
    </div>
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
