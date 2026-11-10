import { stepText, targetText } from '@/lib/plans'

describe('targetText', () => {
  it('names zones', () => {
    expect(targetText({ metric: 'heart_rate', zone: 2 }, 'running')).toBe('HR zone 2')
    expect(targetText({ metric: 'power', zone: 4 }, 'cycling')).toBe('Power zone 4')
  })

  it('renders ranges in the sport’s units', () => {
    expect(targetText({ metric: 'heart_rate', low: 139.6, high: 150.2 }, 'running')).toBe(
      '140–150 bpm',
    )
    expect(targetText({ metric: 'power', low: 200, high: 220 }, 'cycling')).toBe('200–220 W')
    expect(targetText({ metric: 'pace', low: 1000 / 256, high: 1000 / 244 }, 'running')).toBe(
      '4:04–4:16 /km',
    )
    expect(targetText({ metric: 'pace', low: 1, high: 1.25 }, 'pool_swimming')).toBe(
      '1:20–1:40 /100 m',
    )
    expect(targetText({ metric: 'pace', low: 8, high: 9 }, 'cycling')).toBe('28.8 km/h–32.4 km/h')
  })
})

describe('stepText', () => {
  it('renders time, distance and open steps', () => {
    expect(
      stepText(
        {
          kind: 'warmup',
          durationType: 'time',
          duration: 600,
          target: { metric: 'heart_rate', zone: 2 },
        },
        'running',
      ),
    ).toBe('Warm-up · 10:00 · HR zone 2')
    expect(
      stepText({ kind: 'interval', durationType: 'distance', duration: 1000 }, 'running'),
    ).toBe('Interval · 1.00 km')
    expect(stepText({ kind: 'cooldown', durationType: 'open' }, 'running')).toBe(
      'Cool-down · until the lap button',
    )
  })
})
