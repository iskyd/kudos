import { formatActivityStart, formatDistance, formatDuration } from '@/lib/format/units'

describe('formatDuration', () => {
  it.each([
    [0, '0:00'],
    [59.6, '1:00'],
    [123, '2:03'],
    [3599, '59:59'],
    [3600, '1:00:00'],
    [3723, '1:02:03'],
    [90000, '25:00:00'],
  ])('%f s is %s', (seconds, want) => {
    expect(formatDuration(seconds)).toBe(want)
  })
})

describe('formatDistance', () => {
  it.each([
    [0, '0.00 km'],
    [999, '1.00 km'],
    [10015, '10.02 km'],
    [42195, '42.20 km'],
  ])('%f m is %s', (metres, want) => {
    expect(formatDistance(metres)).toBe(want)
  })
})

describe('formatActivityStart', () => {
  it('shows the local time where the activity happened', () => {
    // 05:30 UTC in Rome (summer, +2 h) is 07:30.
    expect(formatActivityStart('2026-10-10T05:30:00Z', 7200)).toBe('10 Oct 2026, 07:30')
  })

  it('crosses midnight with the offset', () => {
    expect(formatActivityStart('2026-10-14T23:30:00Z', 7200)).toBe('15 Oct 2026, 01:30')
    expect(formatActivityStart('2026-10-15T02:00:00Z', -18000)).toBe('14 Oct 2026, 21:00')
  })
})
