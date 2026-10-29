import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createContext, useContext } from 'react'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type Me = components['schemas']['Me']
type Credentials = components['schemas']['RegisterRequest']

const meKey = ['me'] as const

// useMe resolves to the logged-in user, or null when no one is logged in.
export function useMe() {
  return useQuery({
    queryKey: meKey,
    queryFn: async (): Promise<Me | null> => {
      const { data, error, response } = await api.GET('/api/me')
      if (response.status === 401) return null
      if (error) throw new ApiError(error)
      return data
    },
  })
}

// Provided by RequireAuth, so pages below it never render without a user, even while logging out.
export const CurrentUserContext = createContext<Me | null>(null)

// useCurrentUser returns the logged-in user. Only use it below RequireAuth.
export function useCurrentUser(): Me {
  const me = useContext(CurrentUserContext)
  if (!me) throw new Error('useCurrentUser used outside RequireAuth')
  return me
}

export function useLogin() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: Credentials) => {
      const { data, error } = await api.POST('/api/auth/login', { body })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: (me) => queryClient.setQueryData(meKey, me),
  })
}

export function useRegister() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (body: Credentials) => {
      const { data, error } = await api.POST('/api/auth/register', { body })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: (me) => queryClient.setQueryData(meKey, me),
  })
}

export function useLogout() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async () => {
      const { error, response } = await api.POST('/api/auth/logout', {
        headers: { 'X-CSRF-Token': csrfToken },
      })
      // An expired session is already logged out.
      if (error && response.status !== 401) throw new ApiError(error)
    },
    onSuccess: () => {
      // Drop every cached response so the next user never sees this user's data.
      queryClient.setQueryData(meKey, null)
      queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== meKey[0] })
    },
  })
}

export function useUpdateMe() {
  const queryClient = useQueryClient()
  const { csrfToken } = useCurrentUser()
  return useMutation({
    mutationFn: async (body: components['schemas']['UpdateMeRequest']) => {
      const { data, error } = await api.PATCH('/api/me', {
        body,
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (error) throw new ApiError(error)
      return data
    },
    onSuccess: (me) => queryClient.setQueryData(meKey, me),
  })
}
