import { useState, type SubmitEvent } from 'react'
import { useCurrentUser, useUpdateMe } from '@/api/auth'
import {
  type GarminBackfill,
  type GarminConnection,
  useDisconnectGarmin,
  useGarminBackfill,
  useGarminBackfillFailures,
  useGarminConnection,
  useResumeGarminBackfill,
  useSetGarminKey,
  useSetGarminSyncInterval,
  useStartGarminBackfill,
  useSyncGarmin,
} from '@/api/garmin'
import { problemMessage } from '@/api/problem'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { formatDateTime, formatDay, toISODay } from '@/lib/format/date'

const syncIntervals = Array.from({ length: 24 }, (_, i) => i + 1)
const defaultBackfillDays = 120
const maxBackfillDays = 365

const pauseMessages: Record<NonNullable<GarminBackfill['pauseReason']>, string> = {
  rate_limited: 'Paused: Garmin is limiting requests. It continues by itself.',
  key_rejected: 'Paused until you enter a new key.',
  disconnected: 'Paused: Garmin is disconnected.',
}

export function SettingsPage() {
  return (
    <div className="flex max-w-xl flex-col gap-8">
      <h1 className="text-2xl font-semibold">Settings</h1>
      <ProfileSection />
      <GarminSection />
    </div>
  )
}

function ProfileSection() {
  const me = useCurrentUser()
  const update = useUpdateMe()
  const [timezone, setTimezone] = useState(me.timezone)
  // The stored zone may be an alias the browser does not list (e.g. Etc/UTC).
  const zones = Intl.supportedValuesOf('timeZone')
  if (!zones.includes(me.timezone)) zones.unshift(me.timezone)

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    update.mutate({ timezone })
  }

  return (
    <section aria-labelledby="profile-heading" className="flex flex-col gap-4">
      <h2 id="profile-heading" className="text-lg font-semibold">
        Profile
      </h2>
      <p className="text-sm text-muted-foreground">
        Logged in as <span className="text-foreground">{me.username}</span>
      </p>
      <form onSubmit={onSubmit} className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor="timezone">Timezone</Label>
          <NativeSelect
            id="timezone"
            value={timezone}
            onChange={(event) => {
              setTimezone(event.target.value)
              update.reset()
            }}
          >
            {zones.map((zone) => (
              <NativeSelectOption key={zone} value={zone}>
                {zone}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        {update.isError && (
          <p role="alert" className="text-sm text-destructive">
            {problemMessage(update.error)}
          </p>
        )}
        {update.isSuccess && (
          <p role="status" className="text-sm text-muted-foreground">
            Saved.
          </p>
        )}
        <Button type="submit" className="self-start" disabled={update.isPending}>
          Save
        </Button>
      </form>
    </section>
  )
}

function GarminSection() {
  const connection = useGarminConnection()
  return (
    <section aria-labelledby="garmin-heading" className="flex flex-col gap-4">
      <h2 id="garmin-heading" className="text-lg font-semibold">
        Garmin
      </h2>
      {connection.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
      {connection.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(connection.error)}
        </p>
      )}
      {connection.data &&
        (connection.data.connected ? (
          <GarminStatus connection={connection.data} />
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              Not connected. In Garmin Connect, open Settings → Personal API, create a key and paste
              it here with the expiry date Garmin shows.
            </p>
            <GarminKeyForm submitLabel="Connect" />
          </>
        ))}
    </section>
  )
}

function GarminStatus({ connection }: { connection: GarminConnection }) {
  const interval = useSetGarminSyncInterval()
  const disconnect = useDisconnectGarmin()
  const syncNow = useSyncGarmin()
  const expiresOn = connection.keyExpiresOn

  // Warnings about the key and failed syncs show in the app-wide Garmin banner.
  return (
    <div className="flex flex-col gap-4">
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
        <dt className="text-muted-foreground">Key expires</dt>
        <dd>{expiresOn ? formatDay(expiresOn) : 'Unknown'}</dd>
        <dt className="text-muted-foreground">Last sync</dt>
        <dd>{connection.lastSyncAt ? formatDateTime(connection.lastSyncAt) : 'Never'}</dd>
      </dl>
      <div className="flex flex-col gap-2">
        <Label htmlFor="sync-interval">Sync every</Label>
        <NativeSelect
          id="sync-interval"
          value={connection.syncIntervalHours ?? 1}
          disabled={interval.isPending}
          onChange={(event) => {
            interval.mutate(Number(event.target.value))
          }}
        >
          {syncIntervals.map((hours) => (
            <NativeSelectOption key={hours} value={hours}>
              {hours === 1 ? '1 hour' : `${String(hours)} hours`}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        {interval.isError && (
          <p role="alert" className="text-sm text-destructive">
            {problemMessage(interval.error)}
          </p>
        )}
      </div>
      <div className="flex flex-col gap-2">
        <Button
          variant="outline"
          className="self-start"
          disabled={syncNow.isPending || connection.status === 'key_rejected'}
          onClick={() => {
            syncNow.mutate()
          }}
        >
          Sync now
        </Button>
        {syncNow.isError && (
          <p role="alert" className="text-sm text-destructive">
            {problemMessage(syncNow.error)}
          </p>
        )}
        {syncNow.isSuccess && (
          <p role="status" className="text-sm text-muted-foreground">
            {syncNow.data.queued ? 'Sync queued.' : 'A sync is already running.'}
          </p>
        )}
      </div>
      <BackfillSection />
      <GarminKeyForm submitLabel="Replace key" />
      {disconnect.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(disconnect.error)}
        </p>
      )}
      <Button
        variant="outline"
        className="self-start"
        disabled={disconnect.isPending}
        onClick={() => {
          disconnect.mutate()
        }}
      >
        Disconnect
      </Button>
    </div>
  )
}

function GarminKeyForm({ submitLabel }: { submitLabel: string }) {
  const setKey = useSetGarminKey()
  const [key, setKeyText] = useState('')
  const [expiresOn, setExpiresOn] = useState('')
  const today = new Date()
  const tomorrow = toISODay(new Date(today.getFullYear(), today.getMonth(), today.getDate() + 1))
  const inAYear = toISODay(new Date(today.getFullYear() + 1, today.getMonth(), today.getDate()))

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    setKey.mutate(
      { key, expiresOn },
      // The key is write-only: never kept in the page once stored.
      {
        onSuccess: () => {
          setKeyText('')
        },
      },
    )
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <Label htmlFor="garmin-key">Personal API key</Label>
        <Input
          id="garmin-key"
          type="password"
          autoComplete="off"
          required
          maxLength={1024}
          value={key}
          onChange={(event) => {
            setKeyText(event.target.value)
            setKey.reset()
          }}
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="garmin-key-expiry">Key expires on</Label>
        <Input
          id="garmin-key-expiry"
          type="date"
          required
          min={tomorrow}
          max={inAYear}
          value={expiresOn}
          onChange={(event) => {
            setExpiresOn(event.target.value)
            setKey.reset()
          }}
        />
      </div>
      {setKey.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(setKey.error)}
        </p>
      )}
      {setKey.isSuccess && (
        <p role="status" className="text-sm text-muted-foreground">
          Key saved.
        </p>
      )}
      <Button type="submit" className="self-start" disabled={setKey.isPending}>
        {submitLabel}
      </Button>
    </form>
  )
}

function BackfillSection() {
  const backfill = useGarminBackfill()
  return (
    <div className="flex flex-col gap-3">
      <h3 className="font-medium">History import</h3>
      {backfill.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
      {backfill.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(backfill.error)}
        </p>
      )}
      {backfill.data &&
        (backfill.data.started ? (
          <BackfillProgress backfill={backfill.data} />
        ) : (
          <StartBackfillForm />
        ))}
    </div>
  )
}

function StartBackfillForm() {
  const start = useStartGarminBackfill()
  const [days, setDays] = useState(String(defaultBackfillDays))

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    start.mutate(Number(days))
  }

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-3">
      <p className="text-sm text-muted-foreground">
        Import your activities and daily health metrics from Garmin, up to a year back.
      </p>
      <div className="flex flex-col gap-2">
        <Label htmlFor="backfill-days">Days of history</Label>
        <Input
          id="backfill-days"
          type="number"
          required
          min={1}
          max={maxBackfillDays}
          value={days}
          onChange={(event) => {
            setDays(event.target.value)
            start.reset()
          }}
          className="w-32"
        />
      </div>
      {start.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(start.error)}
        </p>
      )}
      <Button type="submit" className="self-start" disabled={start.isPending}>
        Import history
      </Button>
    </form>
  )
}

function BackfillProgress({ backfill }: { backfill: GarminBackfill }) {
  const resume = useResumeGarminBackfill()
  const total = backfill.activitiesTotal ?? 0
  const handled = (backfill.activitiesDone ?? 0) + (backfill.activitiesFailed ?? 0)
  const failed = backfill.activitiesFailed ?? 0
  const window =
    backfill.windowStart && backfill.windowEnd
      ? `${formatDay(backfill.windowStart)} to ${formatDay(backfill.windowEnd)}`
      : ''
  const canResume = backfill.status === 'paused' && backfill.pauseReason !== 'rate_limited'

  return (
    <div className="flex flex-col gap-3">
      <p role="status" className="text-sm">
        {backfill.status === 'done'
          ? `Imported ${window}.`
          : backfill.status === 'paused' && backfill.pauseReason
            ? pauseMessages[backfill.pauseReason]
            : `Importing ${window}…`}
      </p>
      <ProgressRow label="Activities" value={handled} max={total} />
      <ProgressRow
        label="Health days"
        value={backfill.daysDone ?? 0}
        max={backfill.daysTotal ?? 0}
      />
      {resume.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(resume.error)}
        </p>
      )}
      {canResume && (
        <Button
          variant="outline"
          className="self-start"
          disabled={resume.isPending}
          onClick={() => {
            resume.mutate(false)
          }}
        >
          Resume
        </Button>
      )}
      {failed > 0 && (
        <BackfillFailures
          count={failed}
          retrying={resume.isPending}
          onRetry={() => {
            resume.mutate(true)
          }}
        />
      )}
    </div>
  )
}

function ProgressRow({ label, value, max }: { label: string; value: number; max: number }) {
  const id = `progress-${label.toLowerCase().replace(/\s+/g, '-')}`
  return (
    <div className="flex flex-col gap-1">
      <div className="flex justify-between text-sm">
        <label htmlFor={id}>{label}</label>
        <span className="text-muted-foreground">
          {value} of {max}
        </span>
      </div>
      <progress
        id={id}
        value={value}
        max={Math.max(max, 1)}
        className="h-2 w-full accent-primary"
      />
    </div>
  )
}

function BackfillFailures({
  count,
  retrying,
  onRetry,
}: {
  count: number
  retrying: boolean
  onRetry: () => void
}) {
  const failures = useGarminBackfillFailures(true)
  const items = failures.data?.pages.flatMap((page) => page.items) ?? []
  return (
    <div className="flex flex-col gap-2">
      <h4 className="text-sm font-medium">
        {count === 1
          ? '1 activity could not be imported'
          : `${String(count)} activities could not be imported`}
      </h4>
      {failures.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
      {failures.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(failures.error)}
        </p>
      )}
      {items.length > 0 && (
        <ul className="flex flex-col gap-1 text-sm">
          {items.map((f) => (
            <li key={f.sourceActivityId}>
              <span>{formatDateTime(f.startTime)}</span>
              <span className="text-muted-foreground"> — {f.reason}</span>
            </li>
          ))}
        </ul>
      )}
      <div className="flex gap-2">
        {failures.hasNextPage && (
          <Button
            variant="ghost"
            disabled={failures.isFetchingNextPage}
            onClick={() => {
              void failures.fetchNextPage()
            }}
          >
            Show more
          </Button>
        )}
        <Button variant="outline" disabled={retrying} onClick={onRetry}>
          Retry failed
        </Button>
      </div>
    </div>
  )
}
