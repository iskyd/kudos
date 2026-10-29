export type CredentialErrors = { username?: string; password?: string }

// Mirrors the server's rules so most mistakes are caught before a request.
const usernamePattern = /^[A-Za-z0-9._-]{3,32}$/

export function validateCredentials(
  username: string,
  password: string,
  mode: 'login' | 'register',
): CredentialErrors {
  const errors: CredentialErrors = {}
  if (username === '') errors.username = 'Enter a username.'
  else if (mode === 'register' && !usernamePattern.test(username))
    errors.username = 'Use 3 to 32 letters, digits, ".", "_" or "-".'
  if (password === '') errors.password = 'Enter a password.'
  else if (mode === 'register' && (password.length < 8 || password.length > 256))
    errors.password = 'Use 8 to 256 characters.'
  return errors
}
