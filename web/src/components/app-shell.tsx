import { NavLink, Outlet } from 'react-router'
import { useCurrentUser, useLogout } from '@/api/auth'
import { problemMessage } from '@/api/problem'
import { GarminBanner } from '@/components/garmin-banner'
import { navItems } from '@/components/nav-items'
import { Button, buttonVariants } from '@/components/ui/button'

export function AppShell() {
  const me = useCurrentUser()
  const logout = useLogout()

  return (
    <div className="flex min-h-svh bg-background text-foreground">
      <nav aria-label="Main" className="flex w-52 flex-col gap-1 border-r border-border p-3">
        <span className="px-3 py-2 text-lg font-semibold">KudOS</span>
        {navItems.map(({ path, label }) => (
          <NavLink
            key={path}
            to={path}
            end
            className={({ isActive }) =>
              buttonVariants({
                variant: isActive ? 'secondary' : 'ghost',
                className: 'justify-start',
              })
            }
          >
            {label}
          </NavLink>
        ))}
        <div className="mt-auto flex flex-col gap-1">
          <span className="px-3 py-2 text-sm text-muted-foreground">{me.username}</span>
          {logout.isError && (
            <p role="alert" className="px-3 text-sm text-destructive">
              {problemMessage(logout.error)}
            </p>
          )}
          <Button
            variant="ghost"
            className="justify-start"
            disabled={logout.isPending}
            onClick={() => {
              logout.mutate()
            }}
          >
            Log out
          </Button>
        </div>
      </nav>
      <main className="flex-1 p-6">
        <GarminBanner />
        <Outlet />
      </main>
    </div>
  )
}
