import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { ActivitySummary, ManualActivity } from '@/api/activities'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

function listReturns(items: ActivitySummary[], nextCursor?: string) {
  server.use(http.get('/api/activities', () => HttpResponse.json({ items, nextCursor })))
}

const run: ActivitySummary = {
  id: 1,
  sport: 'running',
  subtype: 'outdoor',
  source: 'garmin',
  startTime: '2026-10-10T05:30:00Z',
  utcOffsetS: 7200,
  timerTimeS: 3723,
  elapsedTimeS: 3800,
  distanceM: 10015,
}

async function openDialog(button: string, title: string) {
  renderApp('/activities')
  await userEvent.click(await screen.findByRole('button', { name: button }))
  return screen.findByRole('dialog', { name: title })
}

describe('upload dialog', () => {
  beforeEach(() => {
    loggedInAs(ann)
    listReturns([])
  })

  it('uploads the file with the CSRF token', async () => {
    let content: string | undefined
    let csrf: string | null = null
    server.use(
      http.post('/api/activities/uploads', async ({ request }) => {
        const file = (await request.formData()).get('file')
        // jsdom's File reaches Node's fetch as a nameless blob, so only the content is checked here.
        content = typeof file === 'string' ? undefined : await file?.text()
        csrf = request.headers.get('X-CSRF-Token')
        return HttpResponse.json({ activityIds: [7] }, { status: 201 })
      }),
    )
    const dialog = await openDialog('Upload', 'Upload an activity')
    await userEvent.upload(within(dialog).getByLabelText('File'), new File(['<gpx/>'], 'run.gpx'))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Upload' }))

    expect(await screen.findByRole('status')).toHaveTextContent('Imported 1 activity.')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(content).toBe('<gpx/>')
    expect(csrf).toBe('csrf-ann')
  })

  it('counts every activity of a multisport file', async () => {
    server.use(
      http.post('/api/activities/uploads', () =>
        HttpResponse.json({ activityIds: [1, 2, 3] }, { status: 201 }),
      ),
    )
    const dialog = await openDialog('Upload', 'Upload an activity')
    await userEvent.upload(within(dialog).getByLabelText('File'), new File(['x'], 'tri.fit'))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Upload' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Imported 3 activities.')
  })

  it('asks for a file before uploading', async () => {
    const dialog = await openDialog('Upload', 'Upload an activity')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Upload' }))
    expect(within(dialog).getByText('Choose a file.')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('File')).toHaveAttribute('aria-invalid', 'true')
  })

  it('shows the server error', async () => {
    server.use(
      http.post('/api/activities/uploads', () =>
        problem(
          422,
          'invalid-file',
          'File could not be read',
          'the file could not be read: no session',
        ),
      ),
    )
    const dialog = await openDialog('Upload', 'Upload an activity')
    await userEvent.upload(within(dialog).getByLabelText('File'), new File(['x'], 'broken.fit'))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Upload' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('no session')
  })
})

describe('manual activity dialog', () => {
  beforeEach(() => {
    loggedInAs(ann)
    listReturns([])
  })

  it('sends SI values and the local start time', async () => {
    let body: unknown
    let csrf: string | null = null
    server.use(
      http.post('/api/activities', async ({ request }) => {
        body = await request.json()
        csrf = request.headers.get('X-CSRF-Token')
        return HttpResponse.json({ id: 9 }, { status: 201 })
      }),
    )
    const dialog = await openDialog('Add manually', 'Add an activity')
    await userEvent.selectOptions(within(dialog).getByLabelText('Sport'), 'cycling')
    await userEvent.type(within(dialog).getByLabelText('Start'), '2026-10-29T07:30')
    await userEvent.type(within(dialog).getByLabelText('Duration'), '1:05:30')
    await userEvent.type(within(dialog).getByLabelText('Distance (km)'), '32,5')
    await userEvent.type(within(dialog).getByLabelText('RPE (0–10)'), '6')
    await userEvent.type(within(dialog).getByLabelText('Notes'), 'Windy')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('status')).toHaveTextContent('Activity saved.')
    const want: ManualActivity = {
      sport: 'cycling',
      localStartTime: '2026-10-29T07:30',
      durationS: 3930,
      distanceM: 32500,
      rpe: 6,
      notes: 'Windy',
    }
    expect(body).toEqual(want)
    expect(csrf).toBe('csrf-ann')
  })

  it('leaves out optional fields', async () => {
    let body: unknown
    server.use(
      http.post('/api/activities', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ id: 9 }, { status: 201 })
      }),
    )
    const dialog = await openDialog('Add manually', 'Add an activity')
    await userEvent.type(within(dialog).getByLabelText('Start'), '2026-10-29T18:00')
    await userEvent.type(within(dialog).getByLabelText('Duration'), '45')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await screen.findByRole('status')
    expect(body).toEqual({ sport: 'running', localStartTime: '2026-10-29T18:00', durationS: 2700 })
  })

  it('validates before sending', async () => {
    let called = false
    server.use(
      http.post('/api/activities', () => {
        called = true
        return HttpResponse.json({ id: 1 }, { status: 201 })
      }),
    )
    const dialog = await openDialog('Add manually', 'Add an activity')
    await userEvent.type(within(dialog).getByLabelText('Duration'), '1:75')
    await userEvent.type(within(dialog).getByLabelText('Distance (km)'), 'far')
    await userEvent.type(within(dialog).getByLabelText('RPE (0–10)'), '11')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(within(dialog).getByText('Enter the start date and time.')).toBeInTheDocument()
    expect(within(dialog).getByText('Use h:mm:ss, mm:ss or minutes.')).toBeInTheDocument()
    expect(within(dialog).getByText('Enter kilometres, e.g. 10.5.')).toBeInTheDocument()
    expect(within(dialog).getByText('Use 0 to 10.')).toBeInTheDocument()
    expect(called).toBe(false)
  })

  it('shows the server error', async () => {
    server.use(
      http.post('/api/activities', () =>
        problem(
          400,
          'invalid-request',
          'Invalid request',
          'durationS must be over 0 and at most 7 days',
        ),
      ),
    )
    const dialog = await openDialog('Add manually', 'Add an activity')
    await userEvent.type(within(dialog).getByLabelText('Start'), '2026-10-29T18:00')
    await userEvent.type(within(dialog).getByLabelText('Duration'), '200:00:00')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('at most 7 days')
  })
})

describe('activity list', () => {
  beforeEach(() => {
    loggedInAs(ann)
  })

  it('shows each activity in its local time with formatted values', async () => {
    listReturns([
      run,
      {
        ...run,
        id: 2,
        sport: 'strength',
        subtype: 'indoor',
        timerTimeS: undefined,
        elapsedTimeS: 1800,
        distanceM: undefined,
      },
      {
        ...run,
        id: 3,
        sport: 'transition',
        timerTimeS: undefined,
        elapsedTimeS: undefined,
        distanceM: undefined,
      },
    ])
    renderApp('/activities')
    const rows = await screen.findAllByRole('row')
    expect(rows).toHaveLength(4)
    expect(rows[1]).toHaveTextContent('10 Oct 2026, 07:30Running1:02:0310.02 km')
    expect(rows[2]).toHaveTextContent('Strength30:00—')
    expect(rows[3]).toHaveTextContent('Transition——')
  })

  it('explains an empty list', async () => {
    listReturns([])
    renderApp('/activities')
    expect(await screen.findByText(/No activities yet/)).toBeInTheDocument()
    await userEvent.selectOptions(screen.getByLabelText('Sport'), 'Hiking')
    expect(await screen.findByText('No activities match these filters.')).toBeInTheDocument()
  })

  it('sends the filters and sort order', async () => {
    const queries: string[] = []
    server.use(
      http.get('/api/activities', ({ request }) => {
        queries.push(new URL(request.url).search)
        return HttpResponse.json({ items: [run] })
      }),
    )
    renderApp('/activities')
    await screen.findByRole('cell', { name: 'Running' })
    await userEvent.selectOptions(screen.getByLabelText('Sport'), 'Running')
    await userEvent.type(screen.getByLabelText('From'), '2026-10-01')
    await userEvent.type(screen.getByLabelText('To'), '2026-10-31')
    await userEvent.selectOptions(screen.getByLabelText('Sort by'), 'Longest distance')
    await screen.findByRole('cell', { name: 'Running' })
    expect(queries[0]).toBe('?sort=date')
    expect(queries.at(-1)).toBe('?sort=distance&sport=running&from=2026-10-01&to=2026-10-31')

    await userEvent.selectOptions(screen.getByLabelText('Sport'), 'All sports')
    await userEvent.clear(screen.getByLabelText('From'))
    expect(queries.at(-1)).toBe('?sort=distance&to=2026-10-31')
  })

  it('loads more pages with the cursor', async () => {
    server.use(
      http.get('/api/activities', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor')
        return cursor === 'page-2'
          ? HttpResponse.json({ items: [{ ...run, id: 2, sport: 'cycling' }] })
          : HttpResponse.json({ items: [run], nextCursor: 'page-2' })
      }),
    )
    renderApp('/activities')
    await userEvent.click(await screen.findByRole('button', { name: 'Show more' }))
    expect(await screen.findByRole('cell', { name: 'Cycling' })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: 'Running' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument()
  })

  it('shows loading errors', async () => {
    server.use(http.get('/api/activities', () => problem(500, 'internal', 'Internal error')))
    renderApp('/activities')
    expect(await screen.findByRole('alert')).toHaveTextContent('Internal error')
  })

  it('refreshes after an upload', async () => {
    let calls = 0
    server.use(
      http.get('/api/activities', () => {
        calls++
        return HttpResponse.json({ items: calls > 1 ? [run] : [] })
      }),
      http.post('/api/activities/uploads', () =>
        HttpResponse.json({ activityIds: [1] }, { status: 201 }),
      ),
    )
    const dialog = await openDialog('Upload', 'Upload an activity')
    await userEvent.upload(within(dialog).getByLabelText('File'), new File(['<gpx/>'], 'run.gpx'))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Upload' }))
    expect(await screen.findByRole('cell', { name: 'Running' })).toBeInTheDocument()
  })
})
