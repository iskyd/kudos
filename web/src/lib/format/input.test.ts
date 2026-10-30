import { parseDuration, parseKilometres, parseOptionalNumber, parseTags } from '@/lib/format/input'

describe('parseDuration', () => {
  it.each([
    ['45', 2700],
    ['0.5', 30],
    ['32:15', 1935],
    ['1:05:30', 3930],
    [' 2:00:00 ', 7200],
    ['10:00:00', 36000],
  ])('reads %j as %d s', (text, want) => {
    expect(parseDuration(text)).toBe(want)
  })

  it.each(['', 'abc', '1:2', '1:60', '1:60:00', '-5', '1:00:00:00'])('rejects %j', (text) => {
    expect(parseDuration(text)).toBeNull()
  })
})

describe('parseKilometres', () => {
  it.each([
    ['10', 10000],
    ['21.0975', 21097.5],
    ['5,2', 5200],
    ['0', 0],
  ])('reads %j as %d m', (text, want) => {
    expect(parseKilometres(text)).toBe(want)
  })

  it.each(['', 'ten', '-1', '1.2.3', '1e3'])('rejects %j', (text) => {
    expect(parseKilometres(text)).toBeNull()
  })
})

describe('parseOptionalNumber', () => {
  it.each([
    ['', undefined],
    ['  ', undefined],
    ['7', 7],
    ['6.5', 6.5],
    ['6,5', 6.5],
    ['-2.5', -2.5],
    ['abc', null],
    ['1e3', null],
  ])('%j is %j', (text, want) => {
    expect(parseOptionalNumber(text)).toBe(want)
  })
})

describe('parseTags', () => {
  it('trims, lowercases and drops blanks and repeats', () => {
    expect(parseTags(' Left Knee, achilles,,left knee ')).toEqual(['left knee', 'achilles'])
    expect(parseTags('')).toEqual([])
  })
})
