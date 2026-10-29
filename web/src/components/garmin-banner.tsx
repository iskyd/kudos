import { Link, useLocation } from 'react-router'
import { useGarminConnection } from '@/api/garmin'
import { garminAlert } from '@/lib/garmin'
import { cn } from '@/lib/utils'

// GarminBanner shows the Garmin connection's most urgent problem on every page.
// Loading and errors render nothing: Settings shows the connection in full.
export function GarminBanner() {
  const connection = useGarminConnection()
  const { pathname } = useLocation()
  const alert = connection.data ? garminAlert(connection.data, new Date()) : null
  if (!alert) return null
  return (
    <div
      role="alert"
      className={cn(
        'mb-6 rounded-md border px-4 py-3 text-sm',
        alert.tone === 'destructive'
          ? 'border-destructive text-destructive'
          : 'border-warning text-warning',
      )}
    >
      {alert.text}
      {pathname !== '/settings' && (
        <>
          {' '}
          <Link to="/settings" className="underline">
            Open Settings
          </Link>
        </>
      )}
    </div>
  )
}
