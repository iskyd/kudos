import type { components } from '@/api/generated/schema'

export type Problem = components['schemas']['Problem']

// ApiError carries the problem details of a failed API call.
export class ApiError extends Error {
  readonly problem: Problem

  constructor(problem: Problem) {
    super(problem.title)
    this.name = 'ApiError'
    this.problem = problem
  }
}

// problemMessage turns an error thrown by an API hook into a user-facing message.
export function problemMessage(err: unknown): string {
  if (err instanceof ApiError) return err.problem.detail ?? err.problem.title
  return 'Could not reach the server. Try again.'
}
