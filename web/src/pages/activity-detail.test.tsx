import { act, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type {
  ActivityDetail,
  ActivityStreams,
  Lap,
  StrengthSet,
  SwimLength,
} from '@/api/activities'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'
import { hoverChart } from '@/test/setup'

// Shaped like the corpus: the interval run, the pool swim, the strength session,
// the Zwift ride and a triathlon.
const run: ActivityDetail = {
  id: 1,
  sport: 'running',
  subtype: 'outdoor',
  source: 'upload',
  startTime: '2026-10-10T05:30:00Z',
  utcOffsetS: 7200,
  timerTimeS: 3723,
  distanceM: 10015,
  avgSpeedMS: 1000 / 275,
  avgHrBpm: 151.6,
  ascentM: 42,
  caloriesKcal: 690,
  rpe: 7,
  feel: 75,
  painTags: [],
  workout: {
    name: 'Intervals',
    steps: [
      { index: 0, name: 'Warm up', durationType: 'distance', duration: 2000 },
      { index: 1, name: '400 m', durationType: 'distance', duration: 400 },
    ],
  },
  legs: [],
}

const runStreams: ActivityStreams = {
  timeS: [0, 1, 2],
  lat: [45.3, 45.3001, null],
  lon: [-30, -30.0001, null],
  hrBpm: [120, 130, 140],
  speedMS: [3, 0, 4],
  cadence: [170, 172, 174],
  altitudeM: [80, 81, 82],
}

const laps: Lap[] = [
  {
    index: 0,
    startTime: '2026-10-10T05:30:00Z',
    timerTimeS: 600,
    distanceM: 2000,
    avgSpeedMS: 1000 / 300,
    workoutStepIndex: 0,
    intensity: 'warmup',
  },
  {
    index: 1,
    startTime: '2026-10-10T05:40:00Z',
    timerTimeS: 90,
    distanceM: 400,
    avgHrBpm: 172,
    workoutStepIndex: 1,
    intensity: 'active',
  },
  { index: 2, startTime: '2026-10-10T05:42:00Z', timerTimeS: 60, intensity: 'rest' },
]

function serve(detail: ActivityDetail, streams: ActivityStreams, lapItems: Lap[] = []) {
  server.use(
    http.get('/api/activities/:id', () => HttpResponse.json(detail)),
    http.get('/api/activities/:id/streams', () => HttpResponse.json(streams)),
    http.get('/api/activities/:id/laps', () => HttpResponse.json({ items: lapItems })),
    http.get('/api/config', () =>
      HttpResponse.json({ mapTileUrl: 'https://tiles.example/{z}/{x}/{y}.png' }),
    ),
  )
}

function charts() {
  return screen.getAllByRole('img').map((el) => el.getAttribute('aria-label'))
}

describe('ActivityDetailPage', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it('shows a run: summary, map, charts and laps with workout steps', async () => {
    serve(run, runStreams, laps)
    renderApp('/activities/1')
    expect(await screen.findByRole('heading', { name: 'Running' })).toBeInTheDocument()
    expect(screen.getByText('10 Oct 2026, 07:30')).toBeInTheDocument()
    const summary = screen.getByText('Distance').closest('dl')
    expect(summary).toHaveTextContent('Distance10.02 km')
    expect(summary).toHaveTextContent('Duration1:02:03')
    expect(summary).toHaveTextContent('Avg pace4:35 /km')
    expect(summary).toHaveTextContent('Avg heart rate152 bpm')
    expect(summary).toHaveTextContent('Avg power—')

    expect(await screen.findByRole('region', { name: 'Route map' })).toHaveAttribute(
      'data-map',
      'ready',
    )
    expect(charts()).toEqual(['Heart rate chart', 'Pace chart', 'Cadence chart', 'Elevation chart'])
    expect(screen.getByRole('img', { name: 'Heart rate chart' })).toHaveAttribute(
      'data-points',
      '3',
    )

    const rows = within(await screen.findByRole('table', { name: 'Laps' })).getAllByRole('row')
    expect(rows[1]).toHaveTextContent('1Warm up10:002.00 km5:00 /km')
    expect(rows[2]).toHaveTextContent('2400 m1:300.40 km—172 bpm')
    expect(rows[3]).toHaveTextContent('3rest1:00—')
  })

  it('keeps the page when the browser cannot draw maps', async () => {
    serve(run, runStreams, laps)
    server.use(
      http.get('/api/config', () =>
        HttpResponse.json({ mapTileUrl: 'https://no-webgl/{z}/{x}/{y}.png' }),
      ),
    )
    renderApp('/activities/1')
    expect(await screen.findByRole('note')).toHaveTextContent('This browser cannot display maps')
    expect(screen.getByRole('img', { name: 'Heart rate chart' })).toBeInTheDocument()
  })

  it('moves the map marker as a chart is hovered, without rebuilding the charts', async () => {
    serve(run, runStreams, laps)
    renderApp('/activities/1')
    const hr = await screen.findByRole('img', { name: 'Heart rate chart' })
    await screen.findByRole('region', { name: 'Route map' })
    act(() => {
      hoverChart(hr, 1)
    })
    expect(document.body.dataset.marker).toBe('-30.0001,45.3001')
    // The third sample has no position: no marker.
    act(() => {
      hoverChart(hr, 2)
    })
    expect(document.body.dataset.marker).toBeUndefined()
    act(() => {
      hoverChart(hr, 0)
    })
    expect(document.body.dataset.marker).toBe('-30,45.3')
    for (const chart of screen.getAllByRole('img')) {
      expect(chart).toHaveAttribute('data-builds', '1')
    }
  })

  it('never maps a virtual ride, and shows speed and power', async () => {
    serve(
      {
        ...run,
        id: 2,
        sport: 'cycling',
        subtype: 'virtual',
        avgSpeedMS: 7.9,
        avgPowerW: 210,
        workout: undefined,
      },
      {
        timeS: [0, 1],
        lat: [-11.6, -11.6],
        lon: [166.9, 166.9],
        speedMS: [8, 9],
        powerW: [200, 220],
        cadence: [90, 92],
      },
    )
    renderApp('/activities/2')
    expect(await screen.findByRole('heading', { name: 'Cycling' })).toBeInTheDocument()
    expect(await screen.findByRole('img', { name: 'Speed chart' })).toBeInTheDocument()
    expect(charts()).toEqual(['Speed chart', 'Power chart', 'Cadence chart'])
    expect(screen.queryByRole('region', { name: 'Route map' })).not.toBeInTheDocument()
    expect(screen.getByText('Distance').closest('dl')).toHaveTextContent('Avg speed28.4 km/h')
    expect(await screen.findByText('No laps recorded.')).toBeInTheDocument()
  })

  it('shows pool lengths with SWOLF and rest', async () => {
    const lengths: SwimLength[] = [
      {
        index: 0,
        startTime: '2026-10-11T07:00:00Z',
        active: true,
        stroke: 'freestyle',
        timerTimeS: 52,
        strokes: 37,
        swolf: 89,
        avgSpeedMS: 50 / 52,
      },
      { index: 1, startTime: '2026-10-11T07:01:00Z', active: false, timerTimeS: 30 },
    ]
    serve(
      {
        ...run,
        id: 3,
        sport: 'pool_swimming',
        subtype: 'indoor',
        avgSpeedMS: 100 / 105,
        workout: undefined,
      },
      { timeS: [0], hrBpm: [110] },
    )
    server.use(http.get('/api/activities/:id/lengths', () => HttpResponse.json({ items: lengths })))
    renderApp('/activities/3')
    expect(await screen.findByText('Distance')).toBeInTheDocument()
    expect(screen.getByText('Distance').closest('dl')).toHaveTextContent('Avg pace1:45 /100 m')
    const rows = within(await screen.findByRole('table', { name: 'Lengths' })).getAllByRole('row')
    expect(rows[1]).toHaveTextContent('1freestyle0:523789')
    expect(rows[1]).toHaveTextContent('1:44 /100 m')
    expect(rows[2]).toHaveTextContent('2Rest0:30')
    expect(screen.queryByRole('region', { name: 'Route map' })).not.toBeInTheDocument()
  })

  it('shows strength sets', async () => {
    const sets: StrengthSet[] = [
      {
        index: 0,
        startTime: '2026-10-12T07:00:00Z',
        active: true,
        exerciseName: 'Bench press',
        reps: 10,
        weightKg: 60,
        durationS: 40,
      },
      { index: 1, startTime: '2026-10-12T07:01:00Z', active: false, durationS: 90 },
      {
        index: 2,
        startTime: '2026-10-12T07:03:00Z',
        active: true,
        exerciseName: 'Push up',
        reps: 15,
        weightKg: 0,
        durationS: 35,
      },
    ]
    serve(
      {
        ...run,
        id: 4,
        sport: 'strength',
        subtype: 'indoor',
        distanceM: undefined,
        avgSpeedMS: undefined,
        workout: undefined,
      },
      { timeS: [0, 5], hrBpm: [90, 100] },
    )
    server.use(http.get('/api/activities/:id/sets', () => HttpResponse.json({ items: sets })))
    renderApp('/activities/4')
    const rows = within(await screen.findByRole('table', { name: 'Sets' })).getAllByRole('row')
    expect(rows[1]).toHaveTextContent('1Bench press1060 kg0:40')
    expect(rows[2]).toHaveTextContent('2Rest1:30')
    expect(rows[3]).toHaveTextContent('3Push up15—0:35')
    expect(charts()).toEqual(['Heart rate chart'])
  })

  it('links the legs of a triathlon', async () => {
    const leg = (id: number, sport: ActivityDetail['sport']) => ({
      ...run,
      id,
      sport,
      workout: undefined,
      legs: [],
    })
    serve(
      {
        ...leg(6, 'transition'),
        legs: [leg(5, 'open_water_swimming'), leg(6, 'transition'), leg(7, 'cycling')],
      },
      { timeS: [] },
    )
    renderApp('/activities/6')
    const legs = await screen.findByRole('region', { name: 'Legs' })
    expect(within(legs).getByRole('link', { name: 'Open water swimming' })).toHaveAttribute(
      'href',
      '/activities/5',
    )
    expect(within(legs).getByText('Transition')).toHaveAttribute('aria-current', 'page')
    expect(within(legs).getByRole('link', { name: 'Cycling' })).toHaveAttribute(
      'href',
      '/activities/7',
    )
    expect(await screen.findByText('This activity has no recorded samples.')).toBeInTheDocument()
  })

  it('pages through laps', async () => {
    serve(run, runStreams)
    server.use(
      http.get('/api/activities/:id/laps', ({ request }) =>
        new URL(request.url).searchParams.get('cursor') === 'next'
          ? HttpResponse.json({ items: [laps[1]] })
          : HttpResponse.json({ items: [laps[0]], nextCursor: 'next' }),
      ),
    )
    renderApp('/activities/1')
    await userEvent.click(await screen.findByRole('button', { name: 'Show more' }))
    expect(await screen.findByText('400 m')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument()
  })

  it('reports a missing activity', async () => {
    server.use(
      http.get('/api/activities/:id', () =>
        problem(404, 'not-found', 'Not found', 'no such activity'),
      ),
    )
    renderApp('/activities/99')
    expect(await screen.findByRole('alert')).toHaveTextContent('Activity not found.')
  })

  it('reports stream and lap errors', async () => {
    serve(run, runStreams)
    server.use(
      http.get('/api/activities/:id/streams', () => problem(500, 'internal', 'Internal error')),
      http.get('/api/activities/:id/laps', () => problem(500, 'internal', 'Laps failed')),
    )
    renderApp('/activities/1')
    expect(await screen.findByText('Internal error')).toBeInTheDocument()
    expect(await screen.findByText('Laps failed')).toBeInTheDocument()
  })

  it('reports activity load errors', async () => {
    server.use(http.get('/api/activities/:id', () => problem(500, 'internal', 'Internal error')))
    renderApp('/activities/1')
    expect(await screen.findByRole('alert')).toHaveTextContent('Internal error')
  })

  it('is reached from the activity list', async () => {
    serve(run, runStreams, laps)
    server.use(http.get('/api/activities', () => HttpResponse.json({ items: [run] })))
    renderApp('/activities')
    await userEvent.click(await screen.findByRole('link', { name: '10 Oct 2026, 07:30' }))
    expect(await screen.findByRole('heading', { name: 'Running' })).toBeInTheDocument()
  })

  describe('notes and feel', () => {
    it('pre-fills from the watch and saves edits with the CSRF token', async () => {
      serve(run, runStreams)
      let body: unknown
      let csrf: string | null = null
      server.use(
        http.put('/api/activities/:id/annotation', async ({ request }) => {
          body = await request.json()
          csrf = request.headers.get('X-CSRF-Token')
          return HttpResponse.json({ ...run, rpe: 8.5, painTags: ['left knee'] })
        }),
      )
      renderApp('/activities/1')
      const rpe = await screen.findByLabelText('RPE (0–10)')
      expect(rpe).toHaveValue('7')
      expect(screen.getByLabelText('Mood')).toHaveValue('75')
      expect(screen.getByLabelText('Weather')).toHaveValue('')

      await userEvent.clear(rpe)
      await userEvent.type(rpe, '8,5')
      await userEvent.selectOptions(screen.getByLabelText('Mood'), 'Very strong')
      await userEvent.type(
        screen.getByLabelText('Pain tags (comma-separated)'),
        'Left Knee, left knee',
      )
      await userEvent.type(screen.getByLabelText('Temperature (°C)'), '-3')
      await userEvent.selectOptions(screen.getByLabelText('Weather'), 'Snow')
      await userEvent.type(screen.getByLabelText('Notes'), 'Icy')
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))

      expect(await screen.findByText('Saved.')).toBeInTheDocument()
      expect(body).toEqual({
        rpe: 8.5,
        feel: 100,
        notes: 'Icy',
        painTags: ['left knee'],
        weatherTemperatureC: -3,
        weatherCondition: 'snow',
      })
      expect(csrf).toBe('csrf-ann')
    })

    it('clears every field', async () => {
      serve(
        {
          ...run,
          notes: 'Old',
          painTags: ['achilles'],
          weatherTemperatureC: 20,
          weatherCondition: 'sun',
        },
        runStreams,
      )
      let body: unknown
      server.use(
        http.put('/api/activities/:id/annotation', async ({ request }) => {
          body = await request.json()
          return HttpResponse.json({ ...run, rpe: undefined, feel: undefined })
        }),
      )
      renderApp('/activities/1')
      expect(await screen.findByLabelText('Pain tags (comma-separated)')).toHaveValue('achilles')
      await userEvent.clear(screen.getByLabelText('RPE (0–10)'))
      await userEvent.selectOptions(screen.getByLabelText('Mood'), 'Not set')
      await userEvent.clear(screen.getByLabelText('Notes'))
      await userEvent.clear(screen.getByLabelText('Pain tags (comma-separated)'))
      await userEvent.clear(screen.getByLabelText('Temperature (°C)'))
      await userEvent.selectOptions(screen.getByLabelText('Weather'), 'Not set')
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(await screen.findByText('Saved.')).toBeInTheDocument()
      expect(body).toEqual({ painTags: [] })
    })

    it('keeps an unusual watch mood as an option', async () => {
      serve({ ...run, feel: 60 }, runStreams)
      renderApp('/activities/1')
      expect(await screen.findByLabelText('Mood')).toHaveValue('60')
    })

    it.each([
      ['RPE (0–10)', '11', 'RPE must be a number from 0 to 10.'],
      ['RPE (0–10)', 'hard', 'RPE must be a number from 0 to 10.'],
      ['Temperature (°C)', 'warm', 'Temperature must be a number.'],
    ])('rejects %s %j before saving', async (label, text, message) => {
      serve(run, runStreams)
      renderApp('/activities/1')
      const input = await screen.findByLabelText(label)
      await userEvent.clear(input)
      await userEvent.type(input, text)
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(await screen.findByRole('alert')).toHaveTextContent(message)
    })

    it('assigns gear, offering retired gear only where it is assigned', async () => {
      const shoes = { kind: 'shoes', distanceM: 0, timeS: 0, activities: 0, replacementDue: false }
      server.use(
        http.get('/api/gear', () =>
          HttpResponse.json({
            items: [
              { ...shoes, id: 5, name: 'Trainers', retired: false },
              { ...shoes, id: 6, name: 'Old racers', retired: true },
              { ...shoes, id: 7, name: 'Worn out', retired: true },
            ],
          }),
        ),
      )
      serve({ ...run, gearId: 6 }, runStreams)
      let body: unknown
      server.use(
        http.put('/api/activities/:id/annotation', async ({ request }) => {
          body = await request.json()
          return HttpResponse.json({ ...run, gearId: 5 })
        }),
      )
      renderApp('/activities/1')
      const gear = await screen.findByRole('combobox', { name: 'Gear' })
      await screen.findByRole('option', { name: 'Trainers' })
      expect(gear).toHaveValue('6')
      expect(screen.queryByRole('option', { name: 'Worn out' })).not.toBeInTheDocument()

      await userEvent.selectOptions(gear, 'Trainers')
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(await screen.findByText('Saved.')).toBeInTheDocument()
      expect(body).toMatchObject({ gearId: 5 })
    })

    it('shows save errors', async () => {
      serve(run, runStreams)
      server.use(
        http.put('/api/activities/:id/annotation', () =>
          problem(400, 'invalid-request', 'Invalid request', 'at most 10 pain tags'),
        ),
      )
      renderApp('/activities/1')
      await userEvent.click(await screen.findByRole('button', { name: 'Save' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('at most 10 pain tags')
    })
  })
})
