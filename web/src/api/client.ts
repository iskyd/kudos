import createClient from 'openapi-fetch'
import type { paths } from '@/api/generated/schema'

// The API is served from the same origin as the app (proxied by Vite in development).
export const api = createClient<paths>({ baseUrl: window.location.origin })
