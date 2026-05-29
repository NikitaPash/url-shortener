import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders, USER_TOKEN } from '../helpers'
import Login from '../../pages/Login'

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

import { login as loginApi } from '../../api/auth'

describe('Login page', () => {
  it('renders email, password fields and sign-in button', () => {
    renderWithProviders(<Login />)
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument()
  })

  it('renders a link to the register page', () => {
    renderWithProviders(<Login />)
    expect(screen.getByRole('link', { name: /create one/i })).toHaveAttribute('href', '/register')
  })

  it('navigates to /dashboard on successful login', async () => {
    loginApi.mockResolvedValueOnce({ data: { token: USER_TOKEN } })
    const user = userEvent.setup()
    renderWithProviders(<Login />)
    await user.type(screen.getByLabelText(/email/i), 'alice@example.com')
    await user.type(screen.getByLabelText(/password/i), 'password123')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith('/dashboard'))
  })

  it('shows an error message on failed login', async () => {
    loginApi.mockRejectedValueOnce({
      response: { data: { error: 'Invalid email or password' } },
    })
    const user = userEvent.setup()
    renderWithProviders(<Login />)
    await user.type(screen.getByLabelText(/email/i), 'wrong@example.com')
    await user.type(screen.getByLabelText(/password/i), 'wrongpass')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() =>
      expect(screen.getByText(/invalid email or password/i)).toBeInTheDocument()
    )
  })

  it('shows fallback error when server response has no error field', async () => {
    loginApi.mockRejectedValueOnce(new Error('Network Error'))
    const user = userEvent.setup()
    renderWithProviders(<Login />)
    await user.type(screen.getByLabelText(/email/i), 'x@x.com')
    await user.type(screen.getByLabelText(/password/i), 'anypass')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() =>
      expect(screen.getByText(/invalid email or password/i)).toBeInTheDocument()
    )
  })

  it('disables the button while loading', async () => {
    // Never resolves — simulates slow network.
    loginApi.mockImplementationOnce(() => new Promise(() => {}))
    const user = userEvent.setup()
    renderWithProviders(<Login />)
    await user.type(screen.getByLabelText(/email/i), 'a@b.com')
    await user.type(screen.getByLabelText(/password/i), 'password1')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    expect(screen.getByRole('button', { name: /sign in/i })).toBeDisabled()
  })
})
