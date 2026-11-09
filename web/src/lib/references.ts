// A reference the coach cites: an activity or a day of the athlete context.
export type Reference = { kind: 'activity'; id: string } | { kind: 'day'; day: string }

const referencePattern = /\[(act):(\d+)\]|\[(day):(\d{4}-\d{2}-\d{2})\]/g

// splitReferences cuts a reply into text and the references it cites, in order.
export function splitReferences(text: string): (string | Reference)[] {
  const parts: (string | Reference)[] = []
  let last = 0
  for (const m of text.matchAll(referencePattern)) {
    if (m.index > last) parts.push(text.slice(last, m.index))
    parts.push(m[1] ? { kind: 'activity', id: m[2] ?? '' } : { kind: 'day', day: m[4] ?? '' })
    last = m.index + m[0].length
  }
  if (last < text.length) parts.push(text.slice(last))
  return parts
}
