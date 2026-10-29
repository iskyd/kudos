import { useMutation } from '@tanstack/react-query'
import { useCurrentUser } from '@/api/auth'
import { api } from '@/api/client'
import type { components } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type Sport = components['schemas']['Sport']
export type ManualActivity = components['schemas']['ManualActivityRequest']

export function useUploadActivity() {
  const { csrfToken } = useCurrentUser()
  return useMutation({
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
  const { csrfToken } = useCurrentUser()
  return useMutation({
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
