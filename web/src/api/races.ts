import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useCurrentUser } from '@/api/auth'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type Race = components['schemas']['Race']
export type RaceInput = components['schemas']['RaceInput']

const racesKey = ['races'] as const

export function useRaces() {
  return useInfiniteQuery({
    queryKey: racesKey,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/api/races', {
        params: { query: { cursor: pageParam } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    getNextPageParam: (page) => page.nextCursor,
  })
}

// useSaveRace creates a race, or replaces the one with id.
export function useSaveRace(id?: number) {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (body: RaceInput) => {
      const headers = { 'X-CSRF-Token': csrfToken }
      const { data, error } =
        id === undefined
          ? await api.POST('/api/races', { body, headers })
          : await api.PUT('/api/races/{id}', { params: { path: { id } }, body, headers })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: racesKey }),
  })
}

export function useDeleteRace() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (id: number) => {
      const { error } = await api.DELETE('/api/races/{id}', {
        params: { path: { id } },
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: racesKey }),
  })
}
