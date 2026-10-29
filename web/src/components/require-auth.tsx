import { Navigate, Outlet, useLocation } from 'react-router'
import { CurrentUserContext, useMe } from '@/api/auth'
import { problemMessage } from '@/api/problem'

// RequireAuth renders its child routes only for a logged-in user and sends everyone else to login.
export function RequireAuth() {
  const me = useMe()
  const location = useLocation()

  if (me.isPending) return <p className="p-6 text-muted-foreground">Loading…</p>
  if (me.isError) {
    return (
      <p role="alert" className="p-6 text-destructive">
        {problemMessage(me.error)}
      </p>
    )
  }
  if (!me.data) return <Navigate to="/login" replace state={{ from: location.pathname }} />
  return (
    <CurrentUserContext value={me.data}>
      <Outlet />
    </CurrentUserContext>
  )
}
