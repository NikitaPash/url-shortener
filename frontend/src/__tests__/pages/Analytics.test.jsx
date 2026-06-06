import { screen } from '@testing-library/react'
import { renderWithProviders, USER_TOKEN } from '../helpers'
import Analytics from '../../pages/Analytics'

describe('Analytics page', () => {
  it('renders the question textarea and Ask button', () => {
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    expect(screen.getByPlaceholderText(/ask anything/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /ask/i })).toBeInTheDocument()
  })

  it('renders example question chips', () => {
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    expect(screen.getByText(/how many clicks did I get today/i)).toBeInTheDocument()
    expect(screen.getByText(/what countries/i)).toBeInTheDocument()
  })
})
