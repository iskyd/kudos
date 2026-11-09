import { useState, type SubmitEvent } from 'react'
import type { ActivitySport } from '@/api/activities'
import { problemMessage } from '@/api/problem'
import { type Race, type RaceInput, useDeleteRace, useRaces, useSaveRace } from '@/api/races'
import { Empty, QueryStatus } from '@/components/card'
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
import { formatDay, toISODay } from '@/lib/format/date'
import { kilometresText, parseDuration, parseKilometres } from '@/lib/format/input'
import { formatDistance, formatDuration } from '@/lib/format/units'
import { sportLabels } from '@/lib/sports'

const priorityLabels: Record<RaceInput['priority'], string> = {
  A: 'A · season goal',
  B: 'B · tune-up',
  C: 'C · training race',
}

export function RacesPage() {
  const races = useRaces()
  const items = races.data?.pages.flatMap((page) => page.items) ?? []
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold">Races</h1>
        <RaceDialog />
      </div>
      <QueryStatus query={races} />
      {races.isSuccess && items.length === 0 && (
        <Empty>
          No races yet. Add the races you are training for; the coach plans around them.
        </Empty>
      )}
      {items.length > 0 && (
        <ul className="grid gap-4 lg:grid-cols-2">
          {items.map((r) => (
            <RaceItem key={r.id} race={r} />
          ))}
        </ul>
      )}
      {races.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={races.isFetchingNextPage}
          onClick={() => {
            void races.fetchNextPage()
          }}
        >
          Show more
        </Button>
      )}
    </div>
  )
}

function RaceItem({ race: r }: { race: Race }) {
  const remove = useDeleteRace()
  return (
    <li aria-label={r.name} className="flex flex-col gap-2 rounded-md border p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="font-semibold">{r.name}</h2>
          <p className="text-sm text-muted-foreground">
            {formatDay(r.day)} · {sportLabels[r.sport]} · {formatDistance(r.distanceM)}
          </p>
        </div>
        <span className="text-sm font-medium">Priority {r.priority}</span>
      </div>
      <p className="text-sm">
        {r.goalTimeS === undefined ? 'No goal time' : `Goal ${formatDuration(r.goalTimeS)}`}
      </p>
      <div className="flex justify-end gap-2">
        <RaceDialog race={r} />
        <Button
          variant="ghost"
          size="sm"
          aria-label={`Delete ${r.name}`}
          disabled={remove.isPending}
          onClick={() => {
            remove.mutate(r.id)
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
    </li>
  )
}

// RaceDialog adds a race, or edits race when given.
function RaceDialog({ race }: { race?: Race }) {
  const [open, setOpen] = useState(false)
  const [invalid, setInvalid] = useState<string | null>(null)
  const save = useSaveRace(race?.id)
  const title = race ? `Edit ${race.name}` : 'Add race'

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const field = (name: string) => {
      const value = form.get(name)
      return typeof value === 'string' ? value.trim() : ''
    }
    const distanceM = parseKilometres(field('distance'))
    const goal = field('goal')
    const goalTimeS = goal === '' ? undefined : parseDuration(goal)
    if (field('name') === '') {
      setInvalid('Enter a name.')
      return
    }
    if (field('day') === '') {
      setInvalid('Pick the race day.')
      return
    }
    if (distanceM === null || distanceM <= 0) {
      setInvalid('Enter the distance in km.')
      return
    }
    if (goalTimeS === null || (goalTimeS !== undefined && goalTimeS <= 0)) {
      setInvalid('Enter the goal time as h:mm:ss, or leave it empty.')
      return
    }
    setInvalid(null)
    save.mutate(
      {
        name: field('name'),
        day: field('day'),
        sport: field('sport') as ActivitySport,
        distanceM,
        priority: field('priority') as RaceInput['priority'],
        ...(goalTimeS !== undefined && { goalTimeS }),
      },
      {
        onSuccess: () => {
          setOpen(false)
        },
      },
    )
  }

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
        {race ? (
          <Button variant="ghost" size="sm" aria-label={title}>
            Edit
          </Button>
        ) : (
          <Button>Add race</Button>
        )}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>The coach plans your training around your races.</DialogDescription>
        </DialogHeader>
        <form noValidate onSubmit={onSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="race-name">Name</Label>
            <Input id="race-name" name="name" maxLength={200} defaultValue={race?.name} />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="race-day">Day</Label>
            <Input
              id="race-day"
              name="day"
              type="date"
              defaultValue={race?.day ?? toISODay(new Date())}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="race-sport">Sport</Label>
            <NativeSelect id="race-sport" name="sport" defaultValue={race?.sport ?? 'running'}>
              {Object.entries(sportLabels).map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="race-distance">Distance (km)</Label>
            <Input
              id="race-distance"
              name="distance"
              inputMode="decimal"
              defaultValue={race && kilometresText(race.distanceM)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="race-goal">Goal time (h:mm:ss, optional)</Label>
            <Input
              id="race-goal"
              name="goal"
              defaultValue={race?.goalTimeS === undefined ? '' : formatDuration(race.goalTimeS)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="race-priority">Priority</Label>
            <NativeSelect id="race-priority" name="priority" defaultValue={race?.priority ?? 'A'}>
              {Object.entries(priorityLabels).map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
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
