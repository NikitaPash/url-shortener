import { screen } from '@testing-library/react'
import { renderWithProviders } from '../helpers'
import Register from '../../pages/Register'

describe('Register page', () => {
  it('renders email, password fields and create-account button', () => {
    renderWithProviders(<Register />)
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /create account/i })).toBeInTheDocument()
  })

  it('renders a link to the login page', () => {
    renderWithProviders(<Register />)
    expect(screen.getByRole('link', { name: /sign in/i })).toHaveAttribute('href', '/login')
  })
})
