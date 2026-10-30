import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useCurrentUser } from '@/api/auth'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type Sport = components['schemas']['Sport']
export type ManualActivity = components['schemas']['ManualActivityRequest']
export type ActivitySport = components['schemas']['ActivitySport']
export type ActivitySummary = components['schemas']['ActivitySummary']

export interface ActivityFilters {
  sport?: ActivitySport
  from?: string // local day, YYYY-MM-DD
  to?: string
  sort: 'date' | 'distance' | 'duration'
}

const activitiesKey = ['activities'] as const

export function useActivities(filters: ActivityFilters) {
  return useInfiniteQuery({
    queryKey: [...activitiesKey, filters],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/api/activities', {
        params: { query: { ...filters, cursor: pageParam } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    getNextPageParam: (page) => page.nextCursor,
  })
}

export function useUploadActivity() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    onSuccess: () => queryClient.invalidateQueries({ queryKey: activitiesKey }),
    mutationFn: async (file: File) => {
      const form = new FormData()
      form.append('file', file)
      const { data, error } = await api.POST('/api/activities/uploads', {
        // The generated type models the binary field as a string; the serializer sends the File.
        body: { file: '' },
        bodySerializer: () => form,
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
  })
}

export function useCreateActivity() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    onSuccess: () => queryClient.invalidateQueries({ queryKey: activitiesKey }),
    mutationFn: async (body: ManualActivity) => {
      const { data, error } = await api.POST('/api/activities', {
        body,
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
  })
}
