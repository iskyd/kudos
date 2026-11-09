import { Link } from 'react-router'
import { useLLMProvider } from '@/api/llm'
import { useCoachReports } from '@/api/reports'
import { Card, Empty, QueryStatus } from '@/components/card'
import { CoachText } from '@/components/coach-text'
import { formatDay } from '@/lib/format/date'

// InsightCard shows the coach's latest daily insight, written after the morning sync.
export function InsightCard({ today }: { today: string }) {
  const provider = useLLMProvider()
  const insights = useCoachReports('daily_insight', 1)
  const latest = insights.data?.pages[0]?.items[0]
  if (provider.data && !provider.data.configured) {
    return (
      <Card title="Coach insight">
        <Empty>
          <Link to="/settings/ai-coach" className="underline">
            Set up the AI coach
          </Link>{' '}
          for a daily insight after the morning sync.
        </Empty>
      </Card>
    )
  }
  return (
    <Card title="Coach insight">
      <QueryStatus query={insights} />
      {latest && (
        <div className="flex flex-col gap-1 text-sm">
          <p className="text-muted-foreground">
            {latest.day === today ? 'Today' : formatDay(latest.day)}
          </p>
          <CoachText text={latest.content} />
        </div>
      )}
      {insights.isSuccess && latest?.day !== today && (
        <Empty>Today&apos;s insight is written after the morning sync.</Empty>
      )}
    </Card>
  )
}
