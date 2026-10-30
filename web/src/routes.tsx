import type { ReactElement } from 'react'
import type { RouteObject } from 'react-router'
import { AppShell } from '@/components/app-shell'
import { navItems } from '@/components/nav-items'
import { EmptyPage } from '@/components/empty-page'
import { RequireAuth } from '@/components/require-auth'
import { ActivitiesPage } from '@/pages/activities'
import { ActivityDetailPage } from '@/pages/activity-detail'
import { LoginPage, RegisterPage } from '@/pages/auth'
import { SettingsPage } from '@/pages/settings'

const pages: Partial<Record<string, ReactElement>> = {
  '/activities': <ActivitiesPage />,
  '/settings': <SettingsPage />,
}

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
            element: pages[path] ?? <EmptyPage title={label} />,
          })),
          { path: '/activities/:id', element: <ActivityDetailPage /> },
          { path: '*', element: <EmptyPage title="Page not found" /> },
        ],
      },
    ],
  },
]
