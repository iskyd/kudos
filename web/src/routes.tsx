import type { ReactElement } from 'react'
import type { RouteObject } from 'react-router'
import { AppShell } from '@/components/app-shell'
import { navItems } from '@/components/nav-items'
import { EmptyPage } from '@/components/empty-page'
import { RequireAuth } from '@/components/require-auth'
import { ActivitiesPage } from '@/pages/activities'
import { ActivityComparePage } from '@/pages/activity-compare'
import { ActivityDetailPage } from '@/pages/activity-detail'
import { GearPage } from '@/pages/gear'
import { LoginPage, RegisterPage } from '@/pages/auth'
import { SettingsPage } from '@/pages/settings'
import { ZonesPage } from '@/pages/zones'

const pages: Partial<Record<string, ReactElement>> = {
  '/activities': <ActivitiesPage />,
  '/gear': <GearPage />,
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
          { path: '/activities/:id/compare/:other', element: <ActivityComparePage /> },
          { path: '/settings/zones', element: <ZonesPage /> },
          { path: '*', element: <EmptyPage title="Page not found" /> },
        ],
      },
    ],
  },
]
