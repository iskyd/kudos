import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useCurrentUser } from '@/api/auth'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type Goal = components['schemas']['Goal']
export type GoalInput = components['schemas']['GoalInput']

export const goalsKey = ['goals'] as const

export function useGoals() {
  return useInfiniteQuery({
    queryKey: goalsKey,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/api/goals', {
        params: { query: { cursor: pageParam } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    getNextPageParam: (page) => page.nextCursor,
  })
}

export function useCreateGoal() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (body: GoalInput) => {
      const { data, error } = await api.POST('/api/goals', {
        body,
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: goalsKey }),
  })
}

export function useDeleteGoal() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (id: number) => {
      const { error } = await api.DELETE('/api/goals/{id}', {
        params: { path: { id } },
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: goalsKey }),
  })
}
