import { splitReferences } from '@/lib/references'

describe('splitReferences', () => {
  it('cuts out activity and day references in order', () => {
    expect(splitReferences('Easy run [act:12], tired since [day:2026-11-02].')).toEqual([
      'Easy run ',
      { kind: 'activity', id: '12' },
      ', tired since ',
      { kind: 'day', day: '2026-11-02' },
      '.',
    ])
  })

  it('handles references back to back and at the edges', () => {
    expect(splitReferences('[act:1][act:2]')).toEqual([
      { kind: 'activity', id: '1' },
      { kind: 'activity', id: '2' },
    ])
  })

  it('leaves malformed references as text', () => {
    for (const text of ['[act:abc]', '[day:2 Nov]', '[act: 1]', 'plain', '']) {
      expect(splitReferences(text)).toEqual(text ? [text] : [])
    }
  })
})
