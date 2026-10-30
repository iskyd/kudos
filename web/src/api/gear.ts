import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useCurrentUser } from '@/api/auth'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type Gear = components['schemas']['Gear']
export type GearInput = components['schemas']['GearInput']

export const gearKey = ['gear'] as const

// Pages of 100: the first page covers anyone's shoes and bikes, so pickers read only it.
export function useGear() {
  return useInfiniteQuery({
    queryKey: gearKey,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/api/gear', {
        params: { query: { cursor: pageParam, limit: 100 } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    getNextPageParam: (page) => page.nextCursor,
  })
}

export function useSaveGear() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async ({ id, body }: { id?: number; body: GearInput }) => {
      const headers = { 'X-CSRF-Token': csrfToken }
      const { data, error } =
        id === undefined
          ? await api.POST('/api/gear', { body, headers })
          : await api.PUT('/api/gear/{id}', { params: { path: { id } }, body, headers })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: gearKey }),
  })
}

export function useDeleteGear() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (id: number) => {
      const { error } = await api.DELETE('/api/gear/{id}', {
        params: { path: { id } },
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
    },
    // Its activities lose the gear.
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: gearKey })
      await queryClient.invalidateQueries({ queryKey: ['activities'] })
    },
  })
}
