import { zoneRange } from '@/lib/format/zones'

describe('zoneRange', () => {
  it('renders heart rate and power in whole units', () => {
    expect(zoneRange('hr', { name: 'Z1', high: 145.35 })).toBe('below 145 bpm')
    expect(zoneRange('hr', { name: 'Z2', low: 145.35, high: 153.9 })).toBe('145–154 bpm')
    expect(zoneRange('power', { name: 'Neuromuscular', low: 377.5 })).toBe('378 W and above')
    expect(zoneRange('power', { name: 'Odd' })).toBe('—')
  })

  it('renders pace zones slow to fast', () => {
    expect(zoneRange('pace', { name: 'Z1', high: 1000 / 312 })).toBe('slower than 5:12 /km')
    expect(zoneRange('pace', { name: 'Z2', low: 1000 / 312, high: 1000 / 273.6 })).toBe(
      '5:12 /km – 4:34 /km',
    )
    expect(zoneRange('pace', { name: 'Fast', low: 100 / 98 }, 100)).toBe('1:38 /100 m or faster')
    expect(zoneRange('pace', { name: 'Odd' })).toBe('—')
  })
})
