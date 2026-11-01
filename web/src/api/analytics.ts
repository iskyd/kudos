import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { components, operations } from '@/api/generated/schema'
import { ApiError } from '@/api/problem'

export type FitnessDay = components['schemas']['FitnessDay']
export type Volume = components['schemas']['Volume']
export type VolumeQuery = operations['getVolume']['parameters']['query']

const analyticsKey = ['analytics'] as const

// useFitness returns daily load, fitness, fatigue and form for local days from to to.
export function useFitness(from: string, to: string) {
  return useQuery({
    queryKey: [...analyticsKey, 'fitness', from, to],
    queryFn: async () => {
      const { data, error } = await api.GET('/api/fitness', { params: { query: { from, to } } })
      if (error) throw new ApiError(error)
      return data
    },
  })
}

// useVolume returns volume per period with the range's time in zones.
export function useVolume(query: VolumeQuery) {
  return useQuery({
    queryKey: [...analyticsKey, 'volume', query],
    queryFn: async () => {
      const { data, error } = await api.GET('/api/volume', { params: { query } })
      if (error) throw new ApiError(error)
      return data
    },
  })
}

export type Curves = components['schemas']['Curves']
export type Records = components['schemas']['Records']
export type RecordEffort = components['schemas']['Record']['effort']

// useCurves returns the power and pace curves of local days from to to, with their estimates.
export function useCurves(from: string, to: string) {
  return useQuery({
    queryKey: [...analyticsKey, 'curves', from, to],
    queryFn: async () => {
      const { data, error } = await api.GET('/api/curves', { params: { query: { from, to } } })
      if (error) throw new ApiError(error)
      return data
    },
  })
}

// useRecords returns every record with its history.
export function useRecords() {
  return useQuery({
    queryKey: [...analyticsKey, 'records'],
    queryFn: async () => {
      const { data, error } = await api.GET('/api/records')
      if (error) throw new ApiError(error)
      return data
    },
  })
}
