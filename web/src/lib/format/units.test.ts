import {
  formatActivityStart,
  formatDistance,
  formatDuration,
  formatPace,
  formatSpeed,
  formatWhole,
  paceSeconds,
} from '@/lib/format/units'

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

describe('formatPace', () => {
  it.each([
    [1000 / 275, 1000, '4:35 /km'],
    [3.333, 1000, '5:00 /km'],
    [100 / 105, 100, '1:45 /100 m'],
    [0, 1000, '—'],
  ])('%f m/s per %i m is %s', (speed, per, want) => {
    expect(formatPace(speed, per)).toBe(want)
  })

  it('gives raw seconds for charts', () => {
    expect(paceSeconds(4)).toBe(250)
    expect(paceSeconds(2, 100)).toBe(50)
    expect(paceSeconds(0)).toBeNull()
    expect(paceSeconds(-1)).toBeNull()
  })
})

describe('formatSpeed', () => {
  it.each([
    [0, '0.0 km/h'],
    [7.9, '28.4 km/h'],
    [10, '36.0 km/h'],
  ])('%f m/s is %s', (speed, want) => {
    expect(formatSpeed(speed)).toBe(want)
  })
})

describe('formatWhole', () => {
  it('rounds and adds the unit', () => {
    expect(formatWhole(151.6, 'bpm')).toBe('152 bpm')
    expect(formatWhole(0.4, 'W')).toBe('0 W')
    expect(formatWhole(-3.5, 'm')).toBe('-3 m')
  })
})
