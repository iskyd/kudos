// Parsers for form inputs in display units; they return SI values or null when invalid.

// parseDuration reads "h:mm:ss", "mm:ss" or plain minutes ("45") into seconds.
export function parseDuration(text: string): number | null {
  const trimmed = text.trim()
  if (/^\d+(\.\d+)?$/.test(trimmed)) return Number(trimmed) * 60
  const match = /^(?:(\d+):)?(\d{1,2}):(\d{2})$/.exec(trimmed)
  if (!match) return null
  const [, h = '0', m = '0', s = '0'] = match
  if (Number(s) >= 60 || (match[1] !== undefined && Number(m) >= 60)) return null
  return Number(h) * 3600 + Number(m) * 60 + Number(s)
}

// parseKilometres reads a distance in km, with a dot or comma decimal, into metres.
export function parseKilometres(text: string): number | null {
  const trimmed = text.trim().replace(',', '.')
  if (!/^\d+(\.\d+)?$/.test(trimmed)) return null
  return Math.round(Number(trimmed) * 1000 * 1000) / 1000
}
