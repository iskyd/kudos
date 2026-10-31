import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useCurrentUser } from '@/api/auth'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type WellnessDay = components['schemas']['WellnessDay']
export type WellnessDayInput = components['schemas']['WellnessDayInput']
export type HealthLogEntry = components['schemas']['HealthLogEntry']
export type HealthLogInput = components['schemas']['HealthLogInput']

const wellnessKey = ['wellness'] as const
const healthLogKey = ['health-log'] as const

export function useWellnessDays() {
  return useInfiniteQuery({
    queryKey: wellnessKey,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/api/wellness', {
        params: { query: { cursor: pageParam } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    getNextPageParam: (page) => page.nextCursor,
  })
}

export function useSaveWellnessDay() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async ({ day, body }: { day: string; body: WellnessDayInput }) => {
      const { data, error } = await api.PUT('/api/wellness/{day}', {
        params: { path: { day } },
        body,
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: wellnessKey }),
  })
}

export function useDeleteWellnessDay() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (day: string) => {
      const { error } = await api.DELETE('/api/wellness/{day}', {
        params: { path: { day } },
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: wellnessKey }),
  })
}

export function useHealthLog() {
  return useInfiniteQuery({
    queryKey: healthLogKey,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/api/health-log', {
        params: { query: { cursor: pageParam } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    getNextPageParam: (page) => page.nextCursor,
  })
}

export function useSaveHealthLogEntry() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async ({ id, body }: { id?: number; body: HealthLogInput }) => {
      const headers = { 'X-CSRF-Token': csrfToken }
      const { data, error } =
        id === undefined
          ? await api.POST('/api/health-log', { body, headers })
          : await api.PUT('/api/health-log/{entryId}', {
              params: { path: { entryId: id } },
              body,
              headers,
            })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: healthLogKey }),
  })
}

export function useDeleteHealthLogEntry() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (id: number) => {
      const { error } = await api.DELETE('/api/health-log/{entryId}', {
        params: { path: { entryId: id } },
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: healthLogKey }),
  })
}
