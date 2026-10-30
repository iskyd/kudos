import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCurrentUser } from '@/api/auth'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { gearKey } from '@/api/gear'
import { ApiError } from '@/api/problem'

export type Sport = components['schemas']['Sport']
export type ManualActivity = components['schemas']['ManualActivityRequest']
export type ActivitySport = components['schemas']['ActivitySport']
export type ActivitySummary = components['schemas']['ActivitySummary']

export interface ActivityFilters {
  sport?: ActivitySport
  gear?: number
  tag?: string
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

export type ActivityDetail = components['schemas']['ActivityDetail']
export type ActivityStreams = components['schemas']['ActivityStreams']
export type Lap = components['schemas']['Lap']
export type SwimLength = components['schemas']['SwimLength']
export type StrengthSet = components['schemas']['StrengthSet']

// Points per channel requested for charts; full resolution stays on the server.
export const chartPoints = 1000

export function useActivity(id: number) {
  return useQuery({
    queryKey: [...activitiesKey, id],
    queryFn: async () => {
      const { data, error } = await api.GET('/api/activities/{id}', { params: { path: { id } } })
      if (error) throw new ApiError(error)
      return data
    },
  })
}

export function useActivityStreams(id: number) {
  return useQuery({
    queryKey: [...activitiesKey, id, 'streams'],
    queryFn: async () => {
      const { data, error } = await api.GET('/api/activities/{id}/streams', {
        params: { path: { id }, query: { maxPoints: chartPoints } },
      })
      if (error) throw new ApiError(error)
      return data
    },
  })
}

type ChildPath =
  '/api/activities/{id}/laps' | '/api/activities/{id}/lengths' | '/api/activities/{id}/sets'

// T names the item type of the chosen path, which openapi-fetch types only as a union.
// eslint-disable-next-line @typescript-eslint/no-unnecessary-type-parameters
function useChildList<T>(path: ChildPath, id: number, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: [...activitiesKey, id, path],
    enabled,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET(path, {
        params: { path: { id }, query: { cursor: pageParam, limit: 100 } },
      })
      if (error) throw new ApiError(error)
      return data as { items: T[]; nextCursor?: string }
    },
    getNextPageParam: (page) => page.nextCursor,
  })
}

export const useLaps = (id: number) => useChildList<Lap>('/api/activities/{id}/laps', id, true)
export const useSwimLengths = (id: number, enabled: boolean) =>
  useChildList<SwimLength>('/api/activities/{id}/lengths', id, enabled)
export const useStrengthSets = (id: number, enabled: boolean) =>
  useChildList<StrengthSet>('/api/activities/{id}/sets', id, enabled)

export function useConfig() {
  return useQuery({
    queryKey: ['config'],
    staleTime: Infinity,
    queryFn: async () => {
      const { data, error } = await api.GET('/api/config')
      if (error) throw new ApiError(error)
      return data
    },
  })
}

export type ActivityAnnotation = components['schemas']['ActivityAnnotation']

export function useSetAnnotation(id: number) {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (body: ActivityAnnotation) => {
      const { data, error } = await api.PUT('/api/activities/{id}/annotation', {
        params: { path: { id } },
        body,
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: async (detail) => {
      queryClient.setQueryData([...activitiesKey, id], detail)
      // Gear totals and the gear filter depend on the assignment.
      await queryClient.invalidateQueries({ queryKey: gearKey })
      // Lists only: their key holds the filters object, the detail's holds the ID.
      await queryClient.invalidateQueries({
        queryKey: activitiesKey,
        predicate: (q) => typeof q.queryKey[1] === 'object',
      })
    },
  })
}

export type ActivityEdit = components['schemas']['ActivityEdit']
export type ActivityEditRequest = components['schemas']['ActivityEditRequest']

// Edits change the activity, its streams, the list and, for merge and split, a second
// activity and gear totals: everything is refreshed.
function useEditMutation<V>(id: number, run: (v: V, csrf: string) => Promise<ActivityDetail>) {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: (v: V) => run(v, csrfToken),
    onSuccess: async (detail) => {
      queryClient.setQueryData([...activitiesKey, id], detail)
      await queryClient.invalidateQueries({
        queryKey: activitiesKey,
        predicate: (q) => q.queryKey[1] !== id || q.queryKey.length > 2,
      })
      await queryClient.invalidateQueries({ queryKey: gearKey })
    },
  })
}

export function useAddEdit(id: number) {
  return useEditMutation(id, async (body: ActivityEditRequest, csrf) => {
    const { data, error } = await api.POST('/api/activities/{id}/edits', {
      params: { path: { id } },
      body,
      headers: { 'X-CSRF-Token': csrf },
    })
    if (error) throw new ApiError(error)
    return data
  })
}

export function useUndoEdit(id: number) {
  return useEditMutation(id, async (editId: number, csrf) => {
    const { data, error } = await api.DELETE('/api/activities/{id}/edits/{editId}', {
      params: { path: { id, editId } },
      headers: { 'X-CSRF-Token': csrf },
    })
    if (error) throw new ApiError(error)
    return data
  })
}
