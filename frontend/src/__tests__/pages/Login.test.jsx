import { screen } from '@testing-library/react'
import { renderWithProviders } from '../helpers'
import Login from '../../pages/Login'

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
})
