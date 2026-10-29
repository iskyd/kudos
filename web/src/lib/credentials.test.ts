import { validateCredentials } from '@/lib/credentials'

describe('validateCredentials', () => {
  it.each([
    { mode: 'login', username: 'ann', password: 'x', want: {} },
    {
      mode: 'login',
      username: '',
      password: '',
      want: { username: 'Enter a username.', password: 'Enter a password.' },
    },
    { mode: 'login', username: 'a b', password: 'short', want: {} },
    { mode: 'register', username: 'Ann.B_c-1', password: '12345678', want: {} },
    {
      mode: 'register',
      username: 'ab',
      password: '1234567',
      want: {
        username: 'Use 3 to 32 letters, digits, ".", "_" or "-".',
        password: 'Use 8 to 256 characters.',
      },
    },
    {
      mode: 'register',
      username: 'a'.repeat(33),
      password: 'x'.repeat(257),
      want: {
        username: 'Use 3 to 32 letters, digits, ".", "_" or "-".',
        password: 'Use 8 to 256 characters.',
      },
    },
    {
      mode: 'register',
      username: 'ann b',
      password: 'x'.repeat(256),
      want: { username: 'Use 3 to 32 letters, digits, ".", "_" or "-".' },
    },
  ] as const)('$mode $username', ({ mode, username, password, want }) => {
    expect(validateCredentials(username, password, mode)).toEqual(want)
  })
})
