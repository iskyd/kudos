import { useState, type SubmitEvent } from 'react'
import { type Gear, type GearInput, useDeleteGear, useGear, useSaveGear } from '@/api/gear'
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
import { kilometresText, parseKilometres } from '@/lib/format/input'
import { formatDistance, formatDuration } from '@/lib/format/units'

const gearKinds: Record<GearInput['kind'], string> = { shoes: 'Shoes', bike: 'Bike' }

export function GearPage() {
  const gear = useGear()
  const items = gear.data?.pages.flatMap((page) => page.items) ?? []
  const due = items.filter((g) => g.replacementDue && !g.retired)

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold">Gear</h1>
        <GearDialog />
      </div>
      {due.length > 0 && (
        <div role="alert" className="rounded-md border border-destructive p-3 text-sm">
          Time to replace: {due.map((g) => g.name).join(', ')}. They have reached their replacement
          distance.
        </div>
      )}
      {gear.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
      {gear.isError && (
        <p role="alert" className="text-sm text-destructive">
          {problemMessage(gear.error)}
        </p>
      )}
      {gear.isSuccess && items.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No gear yet. Add shoes or a bike, then pick it on an activity.
        </p>
      )}
      {items.length > 0 && (
        <table className="w-full text-left text-sm">
          <thead className="text-muted-foreground">
            <tr>
              <th className="py-2 font-medium">Name</th>
              <th className="py-2 font-medium">Kind</th>
              <th className="py-2 text-right font-medium">Distance</th>
              <th className="py-2 text-right font-medium">Time</th>
              <th className="py-2 text-right font-medium">Activities</th>
              <th className="py-2 text-right font-medium">Replace at</th>
              <th className="py-2" />
            </tr>
          </thead>
          <tbody>
            {items.map((g) => (
              <GearRow key={g.id} gear={g} />
            ))}
          </tbody>
        </table>
      )}
      {gear.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={gear.isFetchingNextPage}
          onClick={() => {
            void gear.fetchNextPage()
          }}
        >
          Show more
        </Button>
      )}
    </div>
  )
}

function GearRow({ gear: g }: { gear: Gear }) {
  const remove = useDeleteGear()
  const [confirming, setConfirming] = useState(false)
  return (
    <tr className="border-t border-border">
      <td className="py-2">
        {g.name}
        {g.retired && <span className="ml-2 text-muted-foreground">(retired)</span>}
      </td>
      <td className="py-2">{gearKinds[g.kind]}</td>
      <td className="py-2 text-right">{formatDistance(g.distanceM)}</td>
      <td className="py-2 text-right">{formatDuration(g.timeS)}</td>
      <td className="py-2 text-right">{g.activities}</td>
      <td className={`py-2 text-right ${g.replacementDue ? 'text-destructive' : ''}`}>
        {g.replacementDistanceM === undefined ? '—' : formatDistance(g.replacementDistanceM)}
      </td>
      <td className="py-2 text-right">
        <div className="flex justify-end gap-2">
          {confirming ? (
            <>
              <Button
                variant="destructive"
                size="sm"
                disabled={remove.isPending}
                onClick={() => {
                  remove.mutate(g.id)
                }}
              >
                Delete {g.name}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setConfirming(false)
                }}
              >
                Cancel
              </Button>
            </>
          ) : (
            <>
              <GearDialog gear={g} />
              <Button
                variant="ghost"
                size="sm"
                aria-label={`Delete ${g.name}`}
                onClick={() => {
                  setConfirming(true)
                }}
              >
                Delete
              </Button>
            </>
          )}
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

// GearDialog adds gear, or edits gear when one is given.
function GearDialog({ gear }: { gear?: Gear }) {
  const [open, setOpen] = useState(false)
  const [invalid, setInvalid] = useState<string | null>(null)
  const save = useSaveGear()

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const field = (name: string) => {
      const value = form.get(name)
      return typeof value === 'string' ? value.trim() : ''
    }
    const name = field('name')
    if (name === '') {
      setInvalid('Enter a name.')
      return
    }
    const threshold = field('replace-at') === '' ? undefined : parseKilometres(field('replace-at'))
    if (threshold === null || threshold === 0) {
      setInvalid('Enter the replacement distance in kilometres, e.g. 800.')
      return
    }
    setInvalid(null)
    save.mutate(
      {
        id: gear?.id,
        body: {
          name,
          kind: field('kind') as GearInput['kind'],
          retired: field('status') === 'retired',
          ...(threshold !== undefined && { replacementDistanceM: threshold }),
        },
      },
      {
        onSuccess: () => {
          setOpen(false)
        },
      },
    )
  }

  const title = gear ? `Edit ${gear.name}` : 'Add gear'
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
        {gear ? (
          <Button variant="ghost" size="sm" aria-label={title}>
            Edit
          </Button>
        ) : (
          <Button>Add gear</Button>
        )}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            Distance and time add up from the activities you assign it to.
          </DialogDescription>
        </DialogHeader>
        <form noValidate onSubmit={onSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="gear-name">Name</Label>
            <Input id="gear-name" name="name" maxLength={80} defaultValue={gear?.name} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="gear-kind">Kind</Label>
            <NativeSelect id="gear-kind" name="kind" defaultValue={gear?.kind ?? 'shoes'}>
              {Object.entries(gearKinds).map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="gear-replace-at">Replace at (km, optional)</Label>
            <Input
              id="gear-replace-at"
              name="replace-at"
              inputMode="decimal"
              defaultValue={
                gear?.replacementDistanceM === undefined
                  ? ''
                  : kilometresText(gear.replacementDistanceM)
              }
            />
          </div>
          {gear && (
            <div className="flex flex-col gap-2">
              <Label htmlFor="gear-status">Status</Label>
              <NativeSelect
                id="gear-status"
                name="status"
                defaultValue={gear.retired ? 'retired' : 'active'}
              >
                <NativeSelectOption value="active">In use</NativeSelectOption>
                <NativeSelectOption value="retired">Retired</NativeSelectOption>
              </NativeSelect>
            </div>
          )}
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
