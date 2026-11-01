import {
  addDays,
  calendarDays,
  daysUntil,
  formatDateTime,
  formatDay,
  mondayOf,
  formatPeriod,
  periodStarts,
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

describe('periodStarts', () => {
  it('lists the periods up to the one holding the day, newest first', () => {
    expect(periodStarts('week', '2026-11-01', 3)).toEqual([
      '2026-10-26',
      '2026-10-19',
      '2026-10-12',
    ])
    expect(periodStarts('month', '2026-02-15', 3)).toEqual([
      '2026-02-01',
      '2026-01-01',
      '2025-12-01',
    ])
    expect(periodStarts('year', '2026-11-01', 2)).toEqual(['2026-01-01', '2025-01-01'])
  })
})

describe('formatPeriod', () => {
  it('names weeks, months and years', () => {
    expect(formatPeriod('week', '2026-10-26')).toBe('Week of 26 October 2026')
    expect(formatPeriod('month', '2026-10-01')).toBe('October 2026')
    expect(formatPeriod('year', '2026-01-01')).toBe('2026')
  })
})

describe('calendarDays', () => {
  // Days are local calendar days (tests run in Europe/Berlin): a summer-time
  // change must not skip or repeat one.
  it('runs where summer time changes', () => {
    expect(new Date(2026, 9, 25, 12).getTimezoneOffset()).toBe(-60)
    expect(new Date(2026, 9, 24, 12).getTimezoneOffset()).toBe(-120)
  })

  it('pads the month to whole weeks from Monday', () => {
    // October 2026 starts on a Thursday and ends on a Saturday; summer time ends on the 25th.
    const days = calendarDays('month', '2026-10-25')
    expect(days).toHaveLength(35)
    expect(days[0]).toBe('2026-09-28')
    expect(days.at(-1)).toBe('2026-11-01')
    expect(days.slice(26, 29)).toEqual(['2026-10-24', '2026-10-25', '2026-10-26'])
    expect(new Set(days).size).toBe(35)
  })

  it('returns the week from Monday', () => {
    expect(calendarDays('week', '2026-10-25')).toEqual([
      '2026-10-19',
      '2026-10-20',
      '2026-10-21',
      '2026-10-22',
      '2026-10-23',
      '2026-10-24',
      '2026-10-25',
    ])
    // Across the spring change, 29 March 2026.
    expect(calendarDays('week', '2026-03-30')[0]).toBe('2026-03-30')
    expect(calendarDays('week', '2026-03-29')).toContain('2026-03-29')
  })

  it('covers six weeks when the month needs them', () => {
    // August 2026 starts on a Saturday and ends on a Monday.
    const days = calendarDays('month', '2026-08-10')
    expect(days).toHaveLength(42)
    expect(days[0]).toBe('2026-07-27')
    expect(days.at(-1)).toBe('2026-09-06')
  })
})
