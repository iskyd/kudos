import type { ReactElement } from 'react'
import type { RouteObject } from 'react-router'
import { AppShell } from '@/components/app-shell'
import { navItems } from '@/components/nav-items'
import { EmptyPage } from '@/components/empty-page'
import { RequireAuth } from '@/components/require-auth'
import { ActivitiesPage } from '@/pages/activities'
import { AICoachPage } from '@/pages/ai-coach'
import { ActivityComparePage } from '@/pages/activity-compare'
import { ActivityDetailPage } from '@/pages/activity-detail'
import { CalendarPage } from '@/pages/calendar'
import { CoachPage } from '@/pages/coach'
import { DashboardPage } from '@/pages/dashboard'
import { GearPage } from '@/pages/gear'
import { GoalsPage } from '@/pages/goals'
import { PlannedWorkoutPage } from '@/pages/planned-workout'
import { PlansPage } from '@/pages/plans'
import { RacesPage } from '@/pages/races'
import { RecordsPage } from '@/pages/records'
import { ReportsPage } from '@/pages/reports'
import { LoginPage, RegisterPage } from '@/pages/auth'
import { SettingsPage } from '@/pages/settings'
import { TrendsPage } from '@/pages/trends'
import { WellnessPage } from '@/pages/wellness'
import { ZonesPage } from '@/pages/zones'

const pages: Partial<Record<string, ReactElement>> = {
  '/': <DashboardPage />,
  '/activities': <ActivitiesPage />,
  '/calendar': <CalendarPage />,
  '/coach': <CoachPage />,
  '/gear': <GearPage />,
  '/goals': <GoalsPage />,
  '/plans': <PlansPage />,
  '/races': <RacesPage />,
  '/records': <RecordsPage />,
  '/reports': <ReportsPage />,
  '/settings': <SettingsPage />,
  '/trends': <TrendsPage />,
  '/wellness': <WellnessPage />,
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
          { path: '/planned/:id', element: <PlannedWorkoutPage /> },
          { path: '/settings/zones', element: <ZonesPage /> },
          { path: '/settings/ai-coach', element: <AICoachPage /> },
          { path: '*', element: <EmptyPage title="Page not found" /> },
        ],
      },
    ],
  },
]
