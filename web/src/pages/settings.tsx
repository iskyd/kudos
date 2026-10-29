import { useState, type SubmitEvent } from 'react'
import { useCurrentUser, useUpdateMe } from '@/api/auth'
import {
  type GarminConnection,
  useDisconnectGarmin,
  useGarminConnection,
  useSetGarminKey,
  useSetGarminSyncInterval,
} from '@/api/garmin'
import { problemMessage } from '@/api/problem'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { daysUntil, formatDateTime, formatDay, toISODay } from '@/lib/format/date'

// Garmin warns about expiring keys a week ahead; so does KudOS.
const expiryWarningDays = 7
const syncIntervals = Array.from({ length: 24 }, (_, i) => i + 1)

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
  const expiresOn = connection.keyExpiresOn
  const daysLeft = expiresOn ? daysUntil(expiresOn, new Date()) : null

  return (
    <div className="flex flex-col gap-4">
      {connection.status === 'key_rejected' ? (
        <p role="alert" className="text-sm text-destructive">
          Garmin rejected the key. Sync is paused until you enter a new key.
        </p>
      ) : daysLeft !== null && daysLeft < 0 ? (
        <p role="alert" className="text-sm text-destructive">
          The key expired. Sync is paused until you enter a new key.
        </p>
      ) : (
        daysLeft !== null &&
        daysLeft <= expiryWarningDays && (
          <p role="alert" className="text-sm text-warning">
            The key expires{' '}
            {daysLeft === 0 ? 'today' : `in ${String(daysLeft)} ${daysLeft === 1 ? 'day' : 'days'}`}
            . Create a new one in Garmin Connect and enter it below.
          </p>
        )
      )}
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
