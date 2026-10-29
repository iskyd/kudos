import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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
