import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCurrentUser } from '@/api/auth'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type PlannedWorkout = components['schemas']['PlannedWorkout']
export type PlannedWorkoutInput = components['schemas']['PlannedWorkoutInput']
export type PlanProposal = components['schemas']['PlanProposal']
export type PlanProposalItem = components['schemas']['PlanProposalItem']
export type PlanDecisions = components['schemas']['PlanDecisions']

const latestProposalKey = ['plan-proposal', 'latest'] as const
// generatingPoll is how often a proposal being generated is checked on.
const generatingPoll = 3000

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

// useLatestPlanProposal is the latest proposal, or null before the first one.
// While it is being generated it is polled until the job fills it in.
export function useLatestPlanProposal() {
  return useQuery({
    queryKey: latestProposalKey,
    queryFn: async () => {
      const { data, error, response } = await api.GET('/api/plan-proposals/latest')
      if (response.status === 404) return null
      if (error) throw new ApiError(error)
      return data
    },
    refetchInterval: (query) =>
      query.state.data?.status === 'generating' ? generatingPoll : false,
  })
}

// useCreatePlanProposal asks the coach for a plan of the next 14 days.
export function useCreatePlanProposal() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST('/api/plan-proposals', {
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: (data) => {
      queryClient.setQueryData(latestProposalKey, data)
    },
  })
}

// useDecidePlanProposal approves and rejects items; approved ones change the plan.
export function useDecidePlanProposal(id: number) {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (body: PlanDecisions) => {
      const { data, error } = await api.POST('/api/plan-proposals/{id}/decisions', {
        params: { path: { id } },
        body,
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: async (data) => {
      queryClient.setQueryData(latestProposalKey, data)
      await Promise.all(
        // The calendar is under analytics.
        ['planned-workouts', 'planned-workout', 'analytics'].map((key) =>
          queryClient.invalidateQueries({ queryKey: [key] }),
        ),
      )
    },
  })
}
