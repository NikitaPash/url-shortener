import { screen } from '@testing-library/react'
import { renderWithProviders, USER_TOKEN, ADMIN_TOKEN } from '../helpers'
import Sidebar from '../../components/Layout/Sidebar'

describe('Sidebar', () => {
  it('renders common navigation links for a regular user', () => {
    renderWithProviders(<Sidebar />, { token: USER_TOKEN })
    expect(screen.getByRole('link', { name: /dashboard/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /my links/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /create link/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /analytics/i })).toBeInTheDocument()
  })

  it('hides the Admin Panel link for a regular user', () => {
    renderWithProviders(<Sidebar />, { token: USER_TOKEN })
    expect(screen.queryByRole('link', { name: /admin panel/i })).not.toBeInTheDocument()
  })

  it('shows the Admin Panel link for an admin user', () => {
    renderWithProviders(<Sidebar />, { token: ADMIN_TOKEN })
    expect(screen.getByRole('link', { name: /admin panel/i })).toBeInTheDocument()
  })

  it('renders the shor.ty brand name', () => {
    renderWithProviders(<Sidebar />, { token: USER_TOKEN })
    expect(screen.getByText(/shor\.ty/i)).toBeInTheDocument()
  })
})
