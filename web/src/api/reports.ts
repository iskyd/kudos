import { useInfiniteQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type CoachReport = components['schemas']['CoachReport']
export type CoachReportKind = components['schemas']['CoachReportKind']

// useCoachReports pages through the coach's insights or reports, latest first.
export function useCoachReports(kind: CoachReportKind, limit?: number) {
  return useInfiniteQuery({
    queryKey: ['coach-reports', kind, limit],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/api/coach-reports', {
        params: { query: { kind, cursor: pageParam, limit } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    getNextPageParam: (page) => page.nextCursor,
  })
}
