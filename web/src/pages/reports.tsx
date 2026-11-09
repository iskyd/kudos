import { Link } from 'react-router'
import { useLLMProvider } from '@/api/llm'
import { useCoachReports } from '@/api/reports'
import { Empty, QueryStatus } from '@/components/card'
import { CoachText } from '@/components/coach-text'
import { Button } from '@/components/ui/button'
import { addDays, formatDay } from '@/lib/format/date'

export function ReportsPage() {
  const provider = useLLMProvider()
  const reports = useCoachReports('weekly_report')
  const items = reports.data?.pages.flatMap((page) => page.items) ?? []
  return (
    <div className="flex max-w-2xl flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold">Reports</h1>
        <p className="text-sm text-muted-foreground">
          Every Monday morning the coach reviews the week before.
        </p>
      </div>
      <QueryStatus query={reports} />
      {reports.isSuccess && items.length === 0 && (
        <Empty>
          No weekly reports yet.
          {provider.data && !provider.data.configured && (
            <>
              {' '}
              <Link to="/settings/ai-coach" className="underline">
                Set up the AI coach
              </Link>{' '}
              to get one.
            </>
          )}
        </Empty>
      )}
      {items.map((r) => {
        // Written on a Monday about the week before.
        const week = `Week of ${formatDay(addDays(r.day, -7))}`
        return (
          <article key={r.id} aria-label={week} className="flex flex-col gap-2">
            <h2 className="text-lg font-semibold">{week}</h2>
            <CoachText text={r.content} />
          </article>
        )
      })}
      {reports.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={reports.isFetchingNextPage}
          onClick={() => {
            void reports.fetchNextPage()
          }}
        >
          Show more
        </Button>
      )}
    </div>
  )
}
