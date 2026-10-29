import type { RouteObject } from 'react-router'
import { AppShell } from '@/components/app-shell'
import { navItems } from '@/components/nav-items'
import { EmptyPage } from '@/components/empty-page'
import { RequireAuth } from '@/components/require-auth'
import { LoginPage, RegisterPage } from '@/pages/auth'
import { SettingsPage } from '@/pages/settings'

export const routes: RouteObject[] = [
  { path: '/login', element: <LoginPage /> },
  { path: '/register', element: <RegisterPage /> },
  {
    element: <RequireAuth />,
    children: [
      {
        element: <AppShell />,
        children: [
          ...navItems.map(({ path, label }) => ({
            path,
            element: path === '/settings' ? <SettingsPage /> : <EmptyPage title={label} />,
          })),
          { path: '*', element: <EmptyPage title="Page not found" /> },
        ],
      },
    ],
  },
]
