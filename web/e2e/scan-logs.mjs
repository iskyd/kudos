// Checks the server log of an end-to-end run (invariant 38): every line is a
// JSON record whose fields are all known to hold IDs, counts, durations or
// request metadata, and no secret, prompt or reply the run used appears in it.
// A new field fails the scan until it is reviewed and added below.
// Usage: node e2e/scan-logs.mjs e2e-results/server.log
import { readFileSync } from 'node:fs'

const fields = new Set([
  // log/slog and the request log
  'time',
  'level',
  'msg',
  'request_id',
  'method',
  'path',
  'status',
  'bytes',
  'duration_ms',
  'user_id',
  'id',
  'addr',
  // Wrapped error messages, which never interpolate values; still checked for secrets below.
  'error',
  // River's own logs: job and queue bookkeeping
  'average_complete_duration',
  'average_queue_wait_duration',
  'average_run_duration',
  'client_id',
  'duration',
  'is_leader',
  'new_num_subscriptions',
  'next_run_at',
  'num_completed_jobs',
  'num_jobs',
  'num_jobs_run',
  'num_jobs_running',
  'num_jobs_stuck',
  'paused',
  'queue',
  'topic',
])

// What the tests send that must never be logged: the Garmin keys and the
// password they use, the fake token's header, the coach's prompt and reply.
const secrets = [
  'e2e-personal-api-key',
  'wrong-key',
  'correct horse battery',
  Buffer.from(JSON.stringify({ alg: 'none' })).toString('base64url'),
  'Plan the days from',
  'Two easy days to start',
  'E2E easy run',
]

function scan(text) {
  const problems = []
  let requests = 0
  text.split('\n').forEach((line, i) => {
    if (line === '') return
    let record
    try {
      record = JSON.parse(line)
    } catch {
      problems.push(`line ${String(i + 1)}: not JSON`)
      return
    }
    if (record.msg === 'request') requests++
    for (const key of Object.keys(record)) {
      if (!fields.has(key))
        problems.push(`line ${String(i + 1)} (${String(record.msg)}): unreviewed field ${key}`)
    }
    for (const s of secrets) {
      if (line.includes(s))
        problems.push(`line ${String(i + 1)}: contains a test secret or coach text`)
    }
  })
  // An empty log would pass every check: the capture must have worked.
  if (requests === 0) problems.push('no request was logged')
  return problems
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const problems = scan(readFileSync(process.argv[2], 'utf8'))
  for (const p of problems) console.error(p)
  if (problems.length > 0) process.exit(1)
  console.log('server log: no secrets, coordinates or health values')
}
