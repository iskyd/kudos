import type { GarminConnection } from '@/api/garmin'
import { daysUntil } from '@/lib/format/date'

// Garmin warns about expiring keys a week ahead; so does KudOS.
export const expiryWarningDays = 7

export interface GarminAlert {
  tone: 'destructive' | 'warning'
  text: string
}

// garminAlert is the one thing the user should know about their Garmin
// connection, most urgent first, or null when all is well.
export function garminAlert(connection: GarminConnection, now: Date): GarminAlert | null {
  if (!connection.connected) return null
  if (connection.status === 'key_rejected') {
    return {
      tone: 'destructive',
      text: 'Garmin rejected the key. Sync is paused until you enter a new key.',
    }
  }
  const daysLeft = connection.keyExpiresOn ? daysUntil(connection.keyExpiresOn, now) : null
  if (daysLeft !== null && daysLeft < 0) {
    return {
      tone: 'destructive',
      text: 'The key expired. Sync is paused until you enter a new key.',
    }
  }
  if (daysLeft !== null && daysLeft <= expiryWarningDays) {
    const when =
      daysLeft === 0 ? 'today' : `in ${String(daysLeft)} ${daysLeft === 1 ? 'day' : 'days'}`
    return {
      tone: 'warning',
      text: `The key expires ${when}. Create a new one in Garmin Connect and enter it in Settings.`,
    }
  }
  if (connection.lastError) {
    return { tone: 'warning', text: `The last Garmin sync failed: ${connection.lastError}` }
  }
  return null
}
