import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCurrentUser } from '@/api/auth'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type GarminConnection = components['schemas']['GarminConnection']
type SetKey = components['schemas']['SetGarminKeyRequest']

const garminKey = ['garmin'] as const

export function useGarminConnection() {
  return useQuery({
    queryKey: garminKey,
    queryFn: async () => {
      const { data, error } = await api.GET('/api/garmin')
      if (error) throw new ApiError(error)
      return data
    },
  })
}

export function useSetGarminKey() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (body: SetKey) => {
      const { data, error } = await api.PUT('/api/garmin/key', {
        body,
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: (connection) => queryClient.setQueryData(garminKey, connection),
  })
}

export function useSetGarminSyncInterval() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (hours: number) => {
      const { data, error } = await api.PUT('/api/garmin/sync-interval', {
        body: { hours },
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: (connection) => queryClient.setQueryData(garminKey, connection),
  })
}

export function useDisconnectGarmin() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async () => {
      const { error } = await api.DELETE('/api/garmin', {
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
    },
    onSuccess: () => queryClient.setQueryData<GarminConnection>(garminKey, { connected: false }),
  })
}

export type GarminBackfill = components['schemas']['GarminBackfill']

const backfillKey = ['garmin', 'backfill'] as const
const failuresKey = ['garmin', 'backfill', 'failures'] as const
// How often a running backfill's progress is refreshed.
const backfillPollMs = 5000

export function useGarminBackfill() {
  return useQuery({
    queryKey: backfillKey,
    queryFn: async () => {
      const { data, error } = await api.GET('/api/garmin/backfill')
      if (error) throw new ApiError(error)
      return data
    },
    refetchInterval: (query) => (query.state.data?.status === 'running' ? backfillPollMs : false),
  })
}

export function useStartGarminBackfill() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (days: number) => {
      const { data, error } = await api.POST('/api/garmin/backfill', {
        body: { days },
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: (backfill) => queryClient.setQueryData(backfillKey, backfill),
  })
}

export function useResumeGarminBackfill() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (retryFailed: boolean) => {
      const { data, error } = await api.POST('/api/garmin/backfill/resume', {
        body: { retryFailed },
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: async (backfill) => {
      queryClient.setQueryData(backfillKey, backfill)
      await queryClient.invalidateQueries({ queryKey: failuresKey })
    },
  })
}

export function useGarminBackfillFailures(enabled: boolean) {
  return useInfiniteQuery({
    queryKey: failuresKey,
    enabled,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/api/garmin/backfill/failures', {
        params: { query: { cursor: pageParam } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    getNextPageParam: (page) => page.nextCursor,
  })
}

export function useSyncGarmin() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST('/api/garmin/sync', {
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: garminKey, exact: true }),
  })
}
