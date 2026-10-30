import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { ActivityDetail, ActivityEdit, ActivitySummary } from '@/api/activities'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

// 05:30 UTC, 07:30 where it happened.
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
}
const trim: ActivityEdit = {
  id: 7,
  kind: 'trim',
  createdAt: '2026-10-11T08:00:00Z',
  from: '2026-10-10T05:35:00Z',
  to: '2026-10-10T05:40:00Z',
}

function serve(detail: ActivityDetail, sameDay: ActivitySummary[] = []) {
  server.use(
    http.get('/api/activities/:id', () => HttpResponse.json(detail)),
    http.get('/api/activities/:id/streams', () => HttpResponse.json({ timeS: [] })),
    http.get('/api/activities/:id/laps', () => HttpResponse.json({ items: [] })),
    http.get('/api/activities', () => HttpResponse.json({ items: [run, ...sameDay] })),
  )
}

// posted records the last edit request and answers with detail.
function posted(detail: ActivityDetail) {
  const sent: { body?: unknown; csrf?: string | null } = {}
  server.use(
    http.post('/api/activities/:id/edits', async ({ request }) => {
      sent.body = await request.json()
      sent.csrf = request.headers.get('X-CSRF-Token')
      return HttpResponse.json(detail)
    }),
  )
  return sent
}

async function editsSection() {
  return within(await screen.findByRole('region', { name: 'Edits' }))
}

beforeEach(() => {
  loggedInAs(ann)
})

describe('ActivityEdits', () => {
  it('trims with offsets from the start and lists the edit', async () => {
    serve(run)
    const sent = posted({ ...run, edits: [trim] })
    renderApp('/activities/1')
    const edits = await editsSection()
    expect(edits.getByText('No edits: everything is as recorded.')).toBeInTheDocument()
    const form = within(edits.getByRole('form', { name: 'Trim' }))
    await userEvent.type(form.getByLabelText('Keep from'), '5:00')
    await userEvent.type(form.getByLabelText('To'), '10')
    await userEvent.click(form.getByRole('button', { name: 'Trim' }))

    expect(await edits.findByText('Trimmed to 07:35:00–07:40:00')).toBeInTheDocument()
    expect(sent).toEqual({ body: { kind: 'trim', fromS: 300, toS: 600 }, csrf: 'csrf-ann' })
  })

  it.each([
    ['', '10:00', 'Use h:mm:ss, mm:ss or minutes from the start.'],
    ['10:00', '5:00', 'The end must come after the start.'],
  ])('rejects heart rate exclusion from %j to %j', async (from, to, message) => {
    serve(run)
    renderApp('/activities/1')
    const form = within((await editsSection()).getByRole('form', { name: 'Exclude heart rate' }))
    if (from) await userEvent.type(form.getByLabelText('Exclude heart rate from'), from)
    await userEvent.type(form.getByLabelText('To'), to)
    await userEvent.click(form.getByRole('button', { name: 'Exclude' }))
    expect((await editsSection()).getByRole('alert')).toHaveTextContent(message)
  })

  it('excludes heart rate', async () => {
    serve(run)
    const sent = posted({ ...run, edits: [{ ...trim, kind: 'exclude_hr' }] })
    renderApp('/activities/1')
    const form = within((await editsSection()).getByRole('form', { name: 'Exclude heart rate' }))
    await userEvent.type(form.getByLabelText('Exclude heart rate from'), '0:00')
    await userEvent.type(form.getByLabelText('To'), '1:00:00')
    await userEvent.click(form.getByRole('button', { name: 'Exclude' }))
    expect(await screen.findByText('Heart rate excluded 07:35:00–07:40:00')).toBeInTheDocument()
    expect(sent.body).toEqual({ kind: 'exclude_hr', fromS: 0, toS: 3600 })
  })

  it('changes the sport', async () => {
    serve(run)
    const sent = posted({
      ...run,
      sport: 'cycling',
      subtype: 'indoor',
      edits: [
        { id: 8, kind: 'sport', createdAt: trim.createdAt, sport: 'cycling', subtype: 'indoor' },
      ],
    })
    renderApp('/activities/1')
    const form = within((await editsSection()).getByRole('form', { name: 'Change sport' }))
    expect(form.getByLabelText('Sport')).toHaveValue('running')
    await userEvent.selectOptions(form.getByLabelText('Sport'), 'Cycling')
    await userEvent.selectOptions(form.getByLabelText('Type'), 'Indoor')
    await userEvent.click(form.getByRole('button', { name: 'Change sport' }))
    expect(await screen.findByText('Sport changed to Cycling (indoor)')).toBeInTheDocument()
    expect(sent.body).toEqual({ kind: 'sport', sport: 'cycling', subtype: 'indoor' })
  })

  it('splits, then offers neither split nor merge again', async () => {
    const other: ActivitySummary = { ...run, id: 2, startTime: '2026-10-10T15:00:00Z' }
    serve(run, [other])
    const sent = posted({
      ...run,
      edits: [
        {
          id: 9,
          kind: 'split',
          createdAt: trim.createdAt,
          at: '2026-10-10T06:00:00Z',
          activityId: 3,
        },
      ],
    })
    renderApp('/activities/1')
    const edits = await editsSection()
    await edits.findByRole('form', { name: 'Merge' })
    await userEvent.type(edits.getByLabelText('Split at'), '30')
    await userEvent.click(edits.getByRole('button', { name: 'Split' }))

    expect(await edits.findByRole('link', { name: 'second part' })).toHaveAttribute(
      'href',
      '/activities/3',
    )
    expect(edits.getByText(/Split at 08:00:00/)).toBeInTheDocument()
    expect(sent.body).toEqual({ kind: 'split', atS: 1800 })
    expect(edits.queryByRole('form', { name: 'Split' })).not.toBeInTheDocument()
    expect(edits.queryByRole('form', { name: 'Merge' })).not.toBeInTheDocument()
  })

  it('rejects a split at the start', async () => {
    serve(run)
    renderApp('/activities/1')
    const edits = await editsSection()
    await userEvent.type(edits.getByLabelText('Split at'), '0')
    await userEvent.click(edits.getByRole('button', { name: 'Split' }))
    expect(edits.getByRole('alert')).toHaveTextContent('Use h:mm:ss')
  })

  it('merges with another activity of the same local day', async () => {
    const queries: string[] = []
    const other: ActivitySummary = { ...run, id: 2, startTime: '2026-10-10T15:00:00Z' }
    serve(run)
    server.use(
      http.get('/api/activities', ({ request }) => {
        queries.push(new URL(request.url).search)
        return HttpResponse.json({ items: [run, other] })
      }),
    )
    const sent = posted({
      ...run,
      edits: [{ id: 10, kind: 'merge', createdAt: trim.createdAt, activityId: 2 }],
    })
    renderApp('/activities/1')
    const form = within(await (await editsSection()).findByRole('form', { name: 'Merge' }))
    expect(form.getByRole('option')).toHaveTextContent('Running, 10 Oct 2026, 17:00')
    await userEvent.click(form.getByRole('button', { name: 'Merge' }))
    expect(await screen.findByRole('link', { name: 'another activity' })).toHaveAttribute(
      'href',
      '/activities/2',
    )
    expect(sent.body).toEqual({ kind: 'merge', activityId: 2 })
    expect(queries[0]).toBe('?sort=date&from=2026-10-10&to=2026-10-10')
  })

  it('undoes an edit', async () => {
    serve({ ...run, edits: [trim] })
    let undone: unknown
    server.use(
      http.delete('/api/activities/:id/edits/:editId', ({ params }) => {
        undone = params.editId
        return HttpResponse.json(run)
      }),
    )
    renderApp('/activities/1')
    const edits = await editsSection()
    await userEvent.click(edits.getByRole('button', { name: 'Undo' }))
    expect(await edits.findByText('No edits: everything is as recorded.')).toBeInTheDocument()
    expect(undone).toBe('7')
  })

  it('shows edit and undo errors', async () => {
    serve({ ...run, edits: [trim] })
    server.use(
      http.post('/api/activities/:id/edits', () =>
        problem(400, 'invalid-request', 'Invalid request', 'nothing would be left'),
      ),
      http.delete('/api/activities/:id/edits/:editId', () =>
        problem(404, 'not-found', 'Not found'),
      ),
    )
    renderApp('/activities/1')
    const edits = await editsSection()
    await userEvent.click(edits.getByRole('button', { name: 'Undo' }))
    expect(await edits.findByText('Not found')).toBeInTheDocument()
    await userEvent.type(edits.getByLabelText('Split at'), '1:00:00')
    await userEvent.click(edits.getByRole('button', { name: 'Split' }))
    expect(await edits.findByText('nothing would be left')).toBeInTheDocument()
  })

  it('sends a merged activity to the one it was merged into', async () => {
    serve({ ...run, mergedInto: 5 })
    renderApp('/activities/1')
    expect(await screen.findByRole('note')).toHaveTextContent(/merged into another activity/)
    expect(screen.getByRole('link', { name: 'another activity' })).toHaveAttribute(
      'href',
      '/activities/5',
    )
    expect(screen.queryByRole('region', { name: 'Edits' })).not.toBeInTheDocument()
  })

  it('links the second part of a split to the first', async () => {
    serve({ ...run, source: 'split', splitOf: 4 })
    renderApp('/activities/1')
    const edits = await editsSection()
    expect(edits.getByRole('link', { name: 'a split activity' })).toHaveAttribute(
      'href',
      '/activities/4',
    )
    expect(edits.queryByRole('form', { name: 'Split' })).not.toBeInTheDocument()
  })
})
