import { NavLink, Outlet } from 'react-router'
import { navItems } from '@/components/nav-items'
import { buttonVariants } from '@/components/ui/button'

export function AppShell() {
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
      </nav>
      <main className="flex-1 p-6">
        <Outlet />
      </main>
    </div>
  )
}
