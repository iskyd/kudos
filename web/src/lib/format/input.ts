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

// kilometresText is the input text for a distance in metres, the inverse of parseKilometres.
export function kilometresText(metres: number): string {
  return String(metres / 1000)
}

// parseOptionalNumber reads an optional number field (dot or comma decimal):
// undefined when blank, null when not a number.
export function parseOptionalNumber(text: string): number | undefined | null {
  const trimmed = text.trim().replace(',', '.')
  if (trimmed === '') return undefined
  if (!/^-?\d+(\.\d+)?$/.test(trimmed)) return null
  return Number(trimmed)
}

// parseTags splits comma-separated tags, trimmed and lowercased, without blanks or repeats.
export function parseTags(text: string): string[] {
  const tags = text.split(',').map((t) => t.trim().toLowerCase())
  return [...new Set(tags.filter((t) => t !== ''))]
}
