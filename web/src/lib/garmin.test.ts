import type { GarminConnection } from '@/api/garmin'
import { garminAlert } from '@/lib/garmin'

const now = new Date(2026, 9, 29, 12)
const ok: GarminConnection = {
  connected: true,
  status: 'connected',
  keyExpiresOn: '2027-01-01',
  syncIntervalHours: 1,
}

describe('garminAlert', () => {
  it('is quiet when all is well or Garmin is not connected', () => {
    expect(garminAlert(ok, now)).toBeNull()
    expect(garminAlert({ connected: false }, now)).toBeNull()
  })

  it.each([
    ['2026-10-29', 'The key expires today.'],
    ['2026-10-30', 'The key expires in 1 day.'],
    ['2026-11-05', 'The key expires in 7 days.'],
  ])('warns when the key expires on %s', (keyExpiresOn, text) => {
    expect(garminAlert({ ...ok, keyExpiresOn }, now)).toEqual({
      tone: 'warning',
      text: `${text} Create a new one in Garmin Connect and enter it in Settings.`,
    })
  })

  it('does not warn 8 days ahead', () => {
    expect(garminAlert({ ...ok, keyExpiresOn: '2026-11-06' }, now)).toBeNull()
  })

  it('reports an expired key', () => {
    expect(garminAlert({ ...ok, keyExpiresOn: '2026-10-28' }, now)?.tone).toBe('destructive')
  })

  it('puts a rejected key before everything else', () => {
    const alert = garminAlert(
      { ...ok, status: 'key_rejected', keyExpiresOn: '2026-10-28', lastError: 'x' },
      now,
    )
    expect(alert?.text).toMatch(/^Garmin rejected the key/)
  })

  it('reports the last sync error', () => {
    expect(garminAlert({ ...ok, lastError: 'Garmin failed: status 503' }, now)).toEqual({
      tone: 'warning',
      text: 'The last Garmin sync failed: Garmin failed: status 503',
    })
  })

  it('handles a connection without an expiry', () => {
    expect(garminAlert({ connected: true, status: 'connected' }, now)).toBeNull()
  })
})
