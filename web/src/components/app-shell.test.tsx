import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { navItems } from '@/components/nav-items'
import { routes } from '@/routes'

function renderAt(path: string) {
  render(<RouterProvider router={createMemoryRouter(routes, { initialEntries: [path] })} />)
}

describe('AppShell', () => {
  it('links every section', () => {
    renderAt('/')
    const nav = screen.getByRole('navigation', { name: 'Main' })
    for (const { label } of navItems) {
      expect(nav).toContainElement(screen.getByRole('link', { name: label }))
    }
  })

  it.each(navItems)('renders $label at $path', ({ path, label }) => {
    renderAt(path)
    expect(screen.getByRole('heading', { name: label })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: label })).toHaveAttribute('aria-current', 'page')
  })

  it('navigates on click', async () => {
    renderAt('/')
    await userEvent.click(screen.getByRole('link', { name: 'Plans' }))
    expect(screen.getByRole('heading', { name: 'Plans' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Dashboard' })).not.toHaveAttribute('aria-current')
  })

  it('renders not found for unknown paths', () => {
    renderAt('/nope')
    expect(screen.getByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
  })
})
