import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders, USER_TOKEN } from '../helpers'
import Register from '../../pages/Register'

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

import { login as loginApi, register as registerApi } from '../../api/auth'

describe('Register page', () => {
  it('renders email, password fields and create-account button', () => {
    renderWithProviders(<Register />)
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /create account/i })).toBeInTheDocument()
  })

  it('shows client-side error when password is shorter than 8 chars (no API call)', async () => {
    const user = userEvent.setup()
    renderWithProviders(<Register />)
    await user.type(screen.getByLabelText(/email/i), 'alice@example.com')
    await user.type(screen.getByLabelText(/password/i), 'short')
    await user.click(screen.getByRole('button', { name: /create account/i }))
    expect(screen.getByText(/at least 8 characters/i)).toBeInTheDocument()
    expect(registerApi).not.toHaveBeenCalled()
  })

  it('calls register API and navigates to /dashboard on success', async () => {
    registerApi.mockResolvedValueOnce({})
    loginApi.mockResolvedValueOnce({ data: { token: USER_TOKEN } })
    const user = userEvent.setup()
    renderWithProviders(<Register />)
    await user.type(screen.getByLabelText(/email/i), 'alice@example.com')
    await user.type(screen.getByLabelText(/password/i), 'validpass')
    await user.click(screen.getByRole('button', { name: /create account/i }))
    await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith('/dashboard'))
    expect(registerApi).toHaveBeenCalledWith('alice@example.com', 'validpass')
  })

  it('shows server error on registration failure', async () => {
    registerApi.mockRejectedValueOnce({
      response: { data: { error: 'email already registered' } },
    })
    const user = userEvent.setup()
    renderWithProviders(<Register />)
    await user.type(screen.getByLabelText(/email/i), 'dup@example.com')
    await user.type(screen.getByLabelText(/password/i), 'password123')
    await user.click(screen.getByRole('button', { name: /create account/i }))
    await waitFor(() =>
      expect(screen.getByText(/email already registered/i)).toBeInTheDocument()
    )
  })

  it('renders a link to the login page', () => {
    renderWithProviders(<Register />)
    expect(screen.getByRole('link', { name: /sign in/i })).toHaveAttribute('href', '/login')
  })
})
