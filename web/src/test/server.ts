import { setupServer } from 'msw/node'

// Tests register their own handlers with server.use; unhandled requests fail the test.
export const server = setupServer()
