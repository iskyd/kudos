import type { RouteObject } from 'react-router'
import { AppShell } from '@/components/app-shell'
import { navItems } from '@/components/nav-items'
import { EmptyPage } from '@/components/empty-page'

export const routes: RouteObject[] = [
  {
    element: <AppShell />,
    children: [
      ...navItems.map(({ path, label }) => ({ path, element: <EmptyPage title={label} /> })),
      { path: '*', element: <EmptyPage title="Page not found" /> },
    ],
  },
]
