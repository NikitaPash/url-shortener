import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders, USER_TOKEN, ADMIN_TOKEN } from '../helpers'
import Sidebar from '../../components/Layout/Sidebar'

vi.mock('../../api/auth', () => ({
  login: vi.fn(),
  register: vi.fn(),
  logout: vi.fn(),
}))

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal()
  return { ...actual, useNavigate: () => mockNavigate }
})

import { logout as logoutApi } from '../../api/auth'

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

  it('clicking Log Out calls the logout API and navigates to /login', async () => {
    logoutApi.mockResolvedValueOnce({})
    const user = userEvent.setup()
    renderWithProviders(<Sidebar />, { token: USER_TOKEN })
    await user.click(screen.getByRole('button', { name: /log out/i }))
    await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith('/login'))
    expect(localStorage.getItem('jwt_token')).toBeNull()
  })

  it('renders the shor.ty brand name', () => {
    renderWithProviders(<Sidebar />, { token: USER_TOKEN })
    expect(screen.getByText(/shor\.ty/i)).toBeInTheDocument()
  })
})
