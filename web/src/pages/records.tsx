import { Link } from 'react-router'
import { type Records, useRecords } from '@/api/analytics'
import { Empty, QueryStatus } from '@/components/card'
import { formatDay } from '@/lib/format/date'
import { formatActivityStart } from '@/lib/format/units'
import { effortLabels, formatEffort } from '@/lib/records'

type Entry = Records['records'][number]['history'][number]

export function RecordsPage() {
  const records = useRecords()
  const data = records.data
  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold">Records</h1>
      <QueryStatus query={records} />
      {data?.since && (
        <p className="text-sm text-muted-foreground">
          Personal bests since {formatDay(data.since)}, from the activities in KudOS.
        </p>
      )}
      {data?.records.length === 0 && (
        <Empty>No records yet. They appear as activities are imported and analysed.</Empty>
      )}
      {data && data.records.length > 0 && (
        <table aria-label="Records" className="w-full text-left text-sm">
          <thead className="text-muted-foreground">
            <tr>
              <th className="py-2 font-medium">Record</th>
              <th className="py-2 text-right font-medium">Best</th>
              <th className="py-2 font-medium">Set</th>
              <th className="py-2 font-medium">History</th>
            </tr>
          </thead>
          <tbody>
            {data.records.map(({ effort, history }) => {
              const best = history.at(-1)
              if (!best) return null
              return (
                <tr key={effort} className="border-t border-border align-top">
                  <td className="py-2">{effortLabels[effort]}</td>
                  <td className="py-2 text-right">{formatEffort(effort, best.value)}</td>
                  <td className="py-2">
                    <ActivityLink entry={best} />
                  </td>
                  <td className="py-2">
                    {history.length > 1 && (
                      <details>
                        <summary>Set {history.length} times</summary>
                        <ol className="mt-1 flex flex-col gap-1">
                          {history.toReversed().map((e) => (
                            <li key={e.activityId}>
                              {formatEffort(effort, e.value)} on <ActivityLink entry={e} />
                            </li>
                          ))}
                        </ol>
                      </details>
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </div>
  )
}

function ActivityLink({ entry }: { entry: Entry }) {
  return (
    <Link to={`/activities/${String(entry.activityId)}`} className="underline">
      {formatActivityStart(entry.startTime, entry.utcOffsetS)}
    </Link>
  )
}
