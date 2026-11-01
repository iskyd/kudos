import { Link } from 'react-router'
import { useActivities } from '@/api/activities'
import { useVolume } from '@/api/analytics'
import { useGarminConnection } from '@/api/garmin'
import { Card, Empty, QueryStatus } from '@/components/card'
import { FitnessCard } from '@/components/fitness-card'
import { ReadinessCard } from '@/components/readiness-card'
import { ActivityRow } from '@/pages/activities'
import { addDays, formatDateTime, formatDay, mondayOf, toISODay } from '@/lib/format/date'
import { formatDistance, formatDuration, formatWhole } from '@/lib/format/units'

// fitnessDays is how far back the fitness and form trend reaches.
const fitnessDays = 90
// volumeWeeks is how many weeks of volume the dashboard lists, this one first.
const volumeWeeks = 12

export function DashboardPage() {
  const today = toISODay(new Date())
  return (
    <div className="flex flex-col gap-8">
      <h1 className="text-2xl font-semibold">Dashboard</h1>
      <div className="grid gap-8 lg:grid-cols-2">
        <ReadinessCard day={today} />
        <FitnessCard from={addDays(today, -(fitnessDays - 1))} to={today} />
        <VolumeCard today={today} />
        <LatestActivitiesCard />
        <SyncCard />
        <Card title="Planned workouts">
          <p className="text-sm text-muted-foreground">
            Your next planned workouts will appear here once training plans are available.
          </p>
        </Card>
        <Card title="Coach insight">
          <p className="text-sm text-muted-foreground">
            The coach&apos;s latest insight will appear here once the AI coach is available.
          </p>
        </Card>
      </div>
    </div>
  )
}

function VolumeCard({ today }: { today: string }) {
  const monday = mondayOf(today)
  const volume = useVolume({
    period: 'week',
    from: addDays(monday, -7 * (volumeWeeks - 1)),
    to: today,
  })
  const byStart = new Map(volume.data?.periods.map((p) => [p.start, p]))
  const weeks = Array.from({ length: volumeWeeks }, (_, i) => addDays(monday, -7 * i))
  return (
    <Card title="Weekly volume">
      <QueryStatus query={volume} />
      {volume.isSuccess && byStart.size === 0 && (
        <Empty>No activities in the last {volumeWeeks} weeks.</Empty>
      )}
      {byStart.size > 0 && (
        <table aria-label="Weekly volume" className="w-full text-left text-sm">
          <thead className="text-muted-foreground">
            <tr>
              <th className="py-2 font-medium">Week of</th>
              <th className="py-2 text-right font-medium">Activities</th>
              <th className="py-2 text-right font-medium">Duration</th>
              <th className="py-2 text-right font-medium">Distance</th>
              <th className="py-2 text-right font-medium">Load</th>
            </tr>
          </thead>
          <tbody>
            {weeks.map((start) => {
              const p = byStart.get(start)
              return (
                <tr key={start} className="border-t border-border">
                  <td className="py-2">{start === monday ? 'This week' : formatDay(start)}</td>
                  <td className="py-2 text-right">{p?.activities ?? 0}</td>
                  <td className="py-2 text-right">{formatDuration(p?.durationS ?? 0)}</td>
                  <td className="py-2 text-right">{formatDistance(p?.distanceM ?? 0)}</td>
                  <td className="py-2 text-right">{Math.round(p?.load ?? 0)}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </Card>
  )
}

function LatestActivitiesCard() {
  const activities = useActivities({ sort: 'date', limit: 5 })
  const items = activities.data?.pages[0]?.items ?? []
  return (
    <Card title="Latest activities">
      <QueryStatus query={activities} />
      {activities.isSuccess && items.length === 0 && (
        <Empty>
          No activities yet.{' '}
          <Link to="/activities" className="underline">
            Add one
          </Link>{' '}
          or connect Garmin in{' '}
          <Link to="/settings" className="underline">
            Settings
          </Link>
          .
        </Empty>
      )}
      {items.length > 0 && (
        <table aria-label="Latest activities" className="w-full text-left text-sm">
          <tbody>
            {items.map((a) => (
              <ActivityRow key={a.id} activity={a} />
            ))}
          </tbody>
        </table>
      )}
    </Card>
  )
}

function SyncCard() {
  const connection = useGarminConnection()
  const c = connection.data
  return (
    <Card title="Sync">
      <QueryStatus query={connection} />
      {c && !c.connected && (
        <Empty>
          Garmin is not connected.{' '}
          <Link to="/settings" className="underline">
            Connect it
          </Link>{' '}
          to import activities and health data.
        </Empty>
      )}
      {c?.connected && (
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          <dt className="text-muted-foreground">Last sync</dt>
          <dd>{c.lastSyncAt ? formatDateTime(c.lastSyncAt) : 'Not yet'}</dd>
          {c.syncedThrough && (
            <>
              <dt className="text-muted-foreground">Synced through</dt>
              <dd>{formatDay(c.syncedThrough)}</dd>
            </>
          )}
          {c.syncIntervalHours !== undefined && (
            <>
              <dt className="text-muted-foreground">Every</dt>
              <dd>{formatWhole(c.syncIntervalHours, 'h')}</dd>
            </>
          )}
          {c.lastError && (
            <>
              <dt className="text-muted-foreground">Last error</dt>
              <dd className="text-destructive">{c.lastError}</dd>
            </>
          )}
        </dl>
      )}
    </Card>
  )
}
