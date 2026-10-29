import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { ManualActivity } from '@/api/activities'
import { ann, loggedInAs, problem, renderApp } from '@/test/render'
import { server } from '@/test/server'

async function openDialog(button: string, title: string) {
  renderApp('/activities')
  await userEvent.click(await screen.findByRole('button', { name: button }))
  return screen.findByRole('dialog', { name: title })
}

describe('upload dialog', () => {
  beforeEach(() => {
    loggedInAs(ann)
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
