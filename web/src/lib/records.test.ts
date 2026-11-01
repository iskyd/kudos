import { effortLabels, formatEffort } from '@/lib/records'

describe('formatEffort', () => {
  it.each([
    ['fastest_5k', 1320.8, '22:01'],
    ['fastest_marathon', 3 * 3600 + 5, '3:00:05'],
    ['fastest_swim_200m', 313.5, '5:14'],
    ['longest_ride', 85234, '85.23 km'],
    ['power_20m', 233.6, '234 W'],
  ] as const)('renders %s %f as %s', (effort, value, want) => {
    expect(formatEffort(effort, value)).toBe(want)
  })

  it('labels every effort', () => {
    expect(effortLabels.fastest_half_marathon).toBe('Fastest half marathon')
    expect(Object.values(effortLabels)).toHaveLength(16)
  })
})
