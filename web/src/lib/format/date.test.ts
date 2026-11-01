import {
  addDays,
  daysUntil,
  formatDateTime,
  formatDay,
  mondayOf,
  toISODay,
} from '@/lib/format/date'

describe('formatDay', () => {
  it('renders the calendar day without shifting it', () => {
    expect(formatDay('2026-10-28')).toBe('28 October 2026')
    expect(formatDay('2027-01-01')).toBe('1 January 2027')
  })
})

describe('formatDateTime', () => {
  it('renders date and time', () => {
    expect(formatDateTime('2026-10-29T07:30:00')).toBe('29 October 2026 at 07:30')
  })
})

describe('daysUntil', () => {
  const now = new Date(2026, 9, 29, 23, 59)
  it.each([
    ['2026-10-29', 0],
    ['2026-10-30', 1],
    ['2026-11-05', 7],
    ['2026-10-28', -1],
    ['2027-10-29', 365],
  ])('%s is %i days away', (date, want) => {
    expect(daysUntil(date, now)).toBe(want)
  })

  it('counts calendar days across a DST change', () => {
    expect(daysUntil('2026-10-26', new Date(2026, 9, 24, 12))).toBe(2)
  })
})

describe('toISODay', () => {
  it('pads month and day', () => {
    expect(toISODay(new Date(2026, 0, 5))).toBe('2026-01-05')
  })
})

describe('addDays', () => {
  it.each([
    ['2026-10-31', 1, '2026-11-01'],
    ['2026-03-01', -1, '2026-02-28'],
    ['2026-10-24', 2, '2026-10-26'], // across the end of summer time
    ['2026-12-31', 0, '2026-12-31'],
  ])('%s plus %i is %s', (date, n, want) => {
    expect(addDays(date, n)).toBe(want)
  })
})

describe('mondayOf', () => {
  it.each([
    ['2026-10-26', '2026-10-26'], // a Monday
    ['2026-11-01', '2026-10-26'], // a Sunday
    ['2026-01-01', '2025-12-29'], // across a year
  ])('the week of %s starts on %s', (date, want) => {
    expect(mondayOf(date)).toBe(want)
  })
})
