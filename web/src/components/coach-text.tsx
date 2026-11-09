import { Link } from 'react-router'
import { formatDay } from '@/lib/format/date'
import { splitReferences } from '@/lib/references'

// CoachText renders the coach's text with its references as links to what they cite.
export function CoachText({ text }: { text: string }) {
  return (
    <p className="whitespace-pre-wrap">
      {splitReferences(text).map((part, i) =>
        typeof part === 'string' ? (
          part
        ) : part.kind === 'activity' ? (
          <Link key={i} to={`/activities/${part.id}`} className="underline">
            activity {part.id}
          </Link>
        ) : (
          <Link key={i} to={`/calendar?day=${part.day}`} className="underline">
            {formatDay(part.day)}
          </Link>
        ),
      )}
    </p>
  )
}
