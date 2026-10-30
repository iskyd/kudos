import { screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import type { ActivityDetail, WorkoutExecution } from '@/api/activities'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

// Shaped like the interval sample: 2 km warm-up, 400 m reps at 3:50–4:00/km.
const run: ActivityDetail = {
  id: 1,
  sport: 'running',
  subtype: 'outdoor',
  source: 'upload',
  startTime: '2026-10-10T05:30:00Z',
  utcOffsetS: 7200,
  painTags: [],
  legs: [],
  edits: [],
  workout: {
    name: 'Intervals',
    steps: [
      { index: 0, intensity: 'warmup', durationType: 'distance', duration: 2000 },
      {
        index: 1,
        name: '400 m',
        durationType: 'distance',
        duration: 400,
        targetType: 'speed',
        targetLow: 1000 / 240,
        targetHigh: 1000 / 230,
      },
      { index: 2, intensity: 'recovery', durationType: 'time', duration: 60 },
    ],
  },
}

function serve(detail: ActivityDetail, execution: WorkoutExecution) {
  server.use(
    http.get('/api/activities/:id', () => HttpResponse.json(detail)),
    http.get('/api/activities/:id/streams', () => HttpResponse.json({ timeS: [] })),
    http.get('/api/activities/:id/laps', () => HttpResponse.json({ items: [] })),
    http.get('/api/activities/:id/execution', () => HttpResponse.json(execution)),
  )
}

function rows(table: string) {
  return within(screen.getByRole('table', { name: table }))
    .getAllByRole('row')
    .map((r) => r.textContent)
}

beforeEach(() => {
  loggedInAs(ann)
})

describe('WorkoutExecutionPanel', () => {
  it('compares each step run with its target', async () => {
    serve(run, {
      kind: 'steps',
      steps: [
        { stepIndex: 0, laps: 2, timerTimeS: 658, distanceM: 2000 },
        {
          stepIndex: 1,
          laps: 1,
          timerTimeS: 89,
          distanceM: 400,
          actual: 400 / 89,
          result: 'above',
        },
        {
          stepIndex: 1,
          laps: 1,
          timerTimeS: 97,
          distanceM: 400,
          actual: 400 / 97,
          result: 'below',
        },
        {
          stepIndex: 1,
          laps: 1,
          timerTimeS: 94,
          distanceM: 400,
          actual: 400 / 94,
          result: 'within',
        },
        { stepIndex: 2, laps: 0, result: 'skipped' },
        { stepIndex: 7, laps: 1 },
      ],
    })
    renderApp('/activities/1')
    await screen.findByRole('region', { name: 'Workout execution' })
    expect(rows('Workout steps')).toEqual([
      'StepLapsDurationDistanceTargetActualResult',
      'warmup210:582.00 km———',
      '400 m11:290.40 km3:50 /km – 4:00 /km3:42 /kmFaster',
      '400 m11:370.40 km3:50 /km – 4:00 /km4:03 /kmSlower',
      '400 m11:340.40 km3:50 /km – 4:00 /km3:55 /kmOn target',
      'recovery0————Skipped',
      'Step 81—————',
    ])
  })

  it('shows target power intervals and compliance', async () => {
    serve(
      { ...run, sport: 'cycling', subtype: 'virtual', workout: undefined },
      {
        kind: 'target_power',
        withinS: 3468,
        totalS: 3600,
        toleranceW: 10,
        intervals: [
          { startS: 0, durationS: 38, targetW: 75, avgPowerW: 74.6, withinS: 38 },
          { startS: 421, durationS: 60, targetW: 135, withinS: 45 },
        ],
      },
    )
    renderApp('/activities/1')
    expect(
      await screen.findByText('96% of the time within 10 W of the target power.'),
    ).toBeInTheDocument()
    expect(rows('Intervals')).toEqual([
      'StartDurationTargetAvg powerOn target',
      '0:000:3875 W75 W100%',
      '7:011:00135 W—75%',
    ])
  })

  it('says when no power was recorded', async () => {
    serve(run, { kind: 'target_power', withinS: 0, totalS: 0, toleranceW: 10, intervals: [] })
    renderApp('/activities/1')
    expect(await screen.findByText('No power was recorded during the workout.')).toBeInTheDocument()
  })

  it('shows nothing without a structured workout', async () => {
    serve(run, { kind: 'none' })
    renderApp('/activities/1')
    await screen.findByText('No laps recorded.')
    expect(screen.queryByRole('region', { name: 'Workout execution' })).not.toBeInTheDocument()
  })

  it('shows errors', async () => {
    serve(run, { kind: 'none' })
    server.use(
      http.get('/api/activities/:id/execution', () => problem(500, 'internal', 'Internal error')),
    )
    renderApp('/activities/1')
    expect(await screen.findByText('Internal error')).toBeInTheDocument()
  })
})
