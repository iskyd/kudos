import { charts, dash, pacePer, speedText } from '@/lib/activity-display'

describe('activity display', () => {
  it.each([
    ['running', 1000],
    ['hiking', 1000],
    ['pool_swimming', 100],
    ['cycling', null],
  ] as const)('shows %s pace per %s m', (sport, per) => {
    expect(pacePer(sport)).toBe(per)
  })

  it('formats speed as pace on foot and speed on a bike', () => {
    expect(speedText('running', 1000 / 275)).toBe('4:35 /km')
    expect(speedText('cycling', 10)).toBe('36.0 km/h')
    expect(speedText('running', undefined)).toBe('—')
  })

  it('dashes missing values', () => {
    expect(dash(undefined, String)).toBe('—')
    expect(dash(3, String)).toBe('3')
  })

  it('charts recorded channels, pace inverted with stops left out', () => {
    const got = charts('running', {
      hrBpm: [120, null],
      speedMS: [4, 0.1],
      cadence: [170, 172],
      altitudeM: [80, 81],
    })
    expect(got.map((c) => c.label)).toEqual(['Heart rate', 'Pace', 'Cadence', 'Elevation'])
    expect(got[1]).toMatchObject({ values: [250, null], invert: true })
    expect(got[2]?.format(172)).toBe('172 spm')
  })

  it('charts speed and rpm for a ride', () => {
    const got = charts('cycling', { speedMS: [10], powerW: [200], cadence: [90] })
    expect(got.map((c) => c.label)).toEqual(['Speed', 'Power', 'Cadence'])
    expect(got[2]?.format(90)).toBe('90 rpm')
  })
})
