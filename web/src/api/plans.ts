import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type PlannedWorkout = components['schemas']['PlannedWorkout']

// usePlannedWorkouts lists the workouts planned from from to to (local days, at most 42).
export function usePlannedWorkouts(from: string, to: string) {
  return useQuery({
    queryKey: ['planned-workouts', from, to],
    queryFn: async () => {
      const { data, error } = await api.GET('/api/planned-workouts', {
        params: { query: { from, to } },
      })
      if (error) throw new ApiError(error)
      return data
    },
  })
}

export function usePlannedWorkout(id: number) {
  return useQuery({
    queryKey: ['planned-workout', id],
    queryFn: async () => {
      const { data, error } = await api.GET('/api/planned-workouts/{id}', {
        params: { path: { id } },
      })
      if (error) throw new ApiError(error)
      return data
    },
  })
}
