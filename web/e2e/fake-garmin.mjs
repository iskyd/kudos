// A fake Garmin Personal API for the end-to-end tests: it serves the recorded
// fixtures in testdata/garmin, so no test ever calls Garmin. It accepts one key
// and ignores dates: every range returns the recorded days. Under /llm it is
// also a fake OpenAI-compatible provider whose plans are two fixed workouts.
import { readFileSync } from 'node:fs'
import { createServer } from 'node:http'

const key = 'e2e-personal-api-key'
const port = Number(process.env.FAKE_GARMIN_PORT ?? 4174)
const fixtures = new URL('../../testdata/garmin/', import.meta.url)
const searchPath = '/activitylist-service/activities/search/activities'

// Path prefixes and the fixture each one serves.
const routes = [
  [searchPath, 'activities_search.json'],
  ['/download-service/files/activity/', 'activity_1.zip'],
  ['/usersummary-service/v2/dailysummaries', 'daily_summaries.json'],
  ['/sleep-service/sleep/dailySleepSummariesByDate', 'sleep_summaries.json'],
  ['/hrv-service/hrv/daily/', 'hrv_summaries.json'],
  ['/weight-service/weight/range/', 'weight.json'],
]

function token() {
  const enc = (o) => Buffer.from(JSON.stringify(o)).toString('base64url')
  const exp = Math.floor(Date.now() / 1000) + 24 * 3600
  return `${enc({ alg: 'none' })}.${enc({ exp })}.fake`
}

// plan answers a plan prompt with two workouts on its first two days.
function plan(prompt) {
  const from = /Plan the days from (\d{4}-\d{2}-\d{2})/.exec(prompt)?.[1]
  if (!from) return 'No plan asked for.'
  const next = new Date(`${from}T00:00:00Z`)
  next.setUTCDate(next.getUTCDate() + 1)
  const steps = [{ kind: 'recovery', durationType: 'time', duration: 2700 }]
  return JSON.stringify({
    keep: [],
    workouts: [
      { day: from, sport: 'running', title: 'E2E easy run', steps },
      { day: next.toISOString().slice(0, 10), sport: 'running', title: 'E2E long run', steps },
    ],
  })
}

createServer((req, res) => {
  const url = new URL(req.url ?? '/', `http://localhost:${port}`)
  if (url.pathname === '/healthz') return res.end('ok')
  if (url.pathname === '/llm/v1/chat/completions') {
    let body = ''
    req.on('data', (chunk) => (body += chunk))
    req.on('end', () => {
      const messages = JSON.parse(body).messages
      res.setHeader('Content-Type', 'application/json')
      res.end(
        JSON.stringify({
          choices: [{ message: { content: plan(messages[messages.length - 1].content) } }],
        }),
      )
    })
    return
  }
  if (url.pathname === '/personalapitoken-service/personal-api/personal-api-key/token') {
    if (req.headers.authorization !== `ApiKey ${key}`) {
      res.statusCode = 401
      return res.end()
    }
    return res.end(token())
  }
  if (!req.headers.authorization?.startsWith('Bearer ')) {
    res.statusCode = 401
    return res.end()
  }
  // The recording is the first and only page of the activity search.
  if (url.pathname === searchPath && url.searchParams.get('start') !== '0') {
    res.setHeader('Content-Type', 'application/json')
    return res.end('[]')
  }
  const route = routes.find(([prefix]) => url.pathname.startsWith(prefix))
  if (!route) {
    res.statusCode = 404
    return res.end()
  }
  const file = route[1]
  res.setHeader(
    'Content-Type',
    file.endsWith('.zip') ? 'application/x-zip-compressed' : 'application/json',
  )
  res.end(readFileSync(new URL(file, fixtures)))
}).listen(port, '127.0.0.1')
