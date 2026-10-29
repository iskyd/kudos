import { ApiError, problemMessage } from '@/api/problem'

describe('problemMessage', () => {
  it.each([
    {
      name: 'prefers detail',
      err: new ApiError({ type: 't', title: 'Invalid request', status: 400, detail: 'bad name' }),
      want: 'bad name',
    },
    {
      name: 'falls back to title',
      err: new ApiError({ type: 't', title: 'Username already taken', status: 409 }),
      want: 'Username already taken',
    },
    {
      name: 'network errors',
      err: new TypeError('Failed to fetch'),
      want: 'Could not reach the server. Try again.',
    },
  ])('$name', ({ err, want }) => {
    expect(problemMessage(err)).toBe(want)
  })
})
