import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useCurrentUser } from '@/api/auth'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type ZoneSet = components['schemas']['ZoneSet']
export type ZoneSetInput = components['schemas']['ZoneSetInput']
export type ZoneSport = components['schemas']['ZoneSport']
export type Zones = components['schemas']['Zones']
export type Zone = components['schemas']['Zone']
export type ZoneSuggestion = components['schemas']['ZoneSuggestion']

export const zonesKey = ['zones'] as const

export function useZoneSets() {
  return useInfiniteQuery({
    queryKey: zonesKey,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/api/zones', {
        params: { query: { cursor: pageParam, limit: 50 } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    getNextPageParam: (page) => page.nextCursor,
  })
}

// Activities pick their zones by date, so a changed version can change any of them.
function useInvalidateZones() {
  const queryClient = useQueryClient()
  return async () => {
    await queryClient.invalidateQueries({ queryKey: zonesKey })
    await queryClient.invalidateQueries({ queryKey: ['activities'] })
  }
}

export function useCreateZoneSet() {
  const { csrfToken } = useCurrentUser()
  const invalidate = useInvalidateZones()
  return useMutation({
    mutationFn: async (body: ZoneSetInput) => {
      const { data, error } = await api.POST('/api/zones', {
        body,
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: invalidate,
  })
}

export function useDeleteZoneSet() {
  const { csrfToken } = useCurrentUser()
  const invalidate = useInvalidateZones()
  return useMutation({
    mutationFn: async (zoneSetId: number) => {
      const { error } = await api.DELETE('/api/zones/{zoneSetId}', {
        params: { path: { zoneSetId } },
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
    },
    onSuccess: invalidate,
  })
}

// Pending suggestions sit under zonesKey, so any zone change refreshes them too.
export function useZoneSuggestions() {
  return useInfiniteQuery({
    queryKey: [...zonesKey, 'suggestions'],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/api/zones/suggestions', {
        params: { query: { cursor: pageParam, limit: 50 } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    getNextPageParam: (page) => page.nextCursor,
  })
}

// useAnswerSuggestion confirms (creating a version) or dismisses a suggestion.
export function useAnswerSuggestion() {
  const { csrfToken } = useCurrentUser()
  const invalidate = useInvalidateZones()
  return useMutation({
    mutationFn: async ({ id, confirm }: { id: number; confirm: boolean }) => {
      const params = { path: { suggestionId: id } }
      const headers = { 'X-CSRF-Token': csrfToken }
      const { error } = confirm
        ? await api.POST('/api/zones/suggestions/{suggestionId}/confirm', { params, headers })
        : await api.POST('/api/zones/suggestions/{suggestionId}/dismiss', { params, headers })
      if (error) throw new ApiError(error)
    },
    onSuccess: invalidate,
  })
}
