import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCurrentUser } from '@/api/auth'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type LLMProvider = components['schemas']['LLMProvider']
export type LLMProviderInput = components['schemas']['LLMProviderInput']
export type LLMProviderKind = components['schemas']['LLMProviderKind']

const llmKey = ['llm-provider'] as const

export function useLLMProvider() {
  return useQuery({
    queryKey: llmKey,
    queryFn: async () => {
      const { data, error } = await api.GET('/api/llm-provider')
      if (error) throw new ApiError(error)
      return data
    },
  })
}

export function useSetLLMProvider() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (body: LLMProviderInput) => {
      const { data, error } = await api.PUT('/api/llm-provider', {
        body,
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: (provider) => queryClient.setQueryData(llmKey, provider),
  })
}

export function useDeleteLLMProvider() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async () => {
      const { error } = await api.DELETE('/api/llm-provider', {
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
    },
    onSuccess: () => queryClient.setQueryData(llmKey, { configured: false, keySet: false }),
  })
}
