import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { server } from '@/test/server'

// Listen before test files import modules: openapi-fetch captures fetch when the client is created.
server.listen({ onUnhandledFrame: 'error' })

afterEach(() => {
  cleanup()
  server.resetHandlers()
})
afterAll(() => {
  server.close()
})
