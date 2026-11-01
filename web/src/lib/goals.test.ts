import { formatGoalValue, parseGoalTarget } from '@/lib/goals'

describe('formatGoalValue', () => {
  it.each([
    ['distance', 2_000_000, '2000.00 km'],
    ['duration', 36_000, '10:00:00'],
    ['ascent', 3000.4, '3000 m'],
  ] as const)('renders %s %d as %s', (metric, value, text) => {
    expect(formatGoalValue(metric, value)).toBe(text)
  })
})

describe('parseGoalTarget', () => {
  it.each([
    ['distance', '2000', 2_000_000],
    ['distance', '42,2', 42_200],
    ['duration', '10:00:00', 36_000],
    ['duration', '90', 5400],
    ['ascent', '3000', 3000],
    ['distance', '0', null],
    ['duration', '0:00', null],
    ['ascent', '-5', null],
    ['ascent', '', null],
    ['distance', 'far', null],
  ] as const)('reads %s %j as %s', (metric, text, value) => {
    expect(parseGoalTarget(metric, text)).toBe(value)
  })
})
