import { useState, type SubmitEvent } from 'react'
import { useCurrentUser, useUpdateMe } from '@/api/auth'
import { problemMessage } from '@/api/problem'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

export function SettingsPage() {
  return (
    <div className="flex max-w-xl flex-col gap-8">
      <h1 className="text-2xl font-semibold">Settings</h1>
      <ProfileSection />
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
