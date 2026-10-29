// A fake Garmin Personal API for the end-to-end tests: it serves the recorded
// fixtures in testdata/garmin, so no test ever calls Garmin. It accepts one key
// and ignores dates: every range returns the recorded days.
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

createServer((req, res) => {
  const url = new URL(req.url ?? '/', `http://localhost:${port}`)
  if (url.pathname === '/healthz') return res.end('ok')
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
