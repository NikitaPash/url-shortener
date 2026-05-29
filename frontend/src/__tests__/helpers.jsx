import { render } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { AuthProvider } from '../context/AuthContext'

/**
 * Build a fake JWT whose payload is readable by isAdminToken().
 * The token has no valid signature — only the payload matters for UI tests.
 */
export function makeToken(claims = {}) {
  const payload = btoa(
    JSON.stringify({ sub: 'user-test-id', exp: Math.floor(Date.now() / 1000) + 3600, ...claims })
  )
  return `eyJhbGciOiJIUzI1NiJ9.${payload}.fakesig`
}

export const USER_TOKEN = makeToken({ is_admin: false })
export const ADMIN_TOKEN = makeToken({ is_admin: true })

/**
 * Render `ui` inside MemoryRouter + AuthProvider.
 * Pass `token` to simulate an authenticated user; leave undefined for guest.
 */
export function renderWithProviders(ui, { token, initialEntries = ['/'] } = {}) {
  if (token) {
    localStorage.setItem('jwt_token', token)
  }
  return render(
    <MemoryRouter initialEntries={initialEntries}>
      <AuthProvider>{ui}</AuthProvider>
    </MemoryRouter>
  )
}
