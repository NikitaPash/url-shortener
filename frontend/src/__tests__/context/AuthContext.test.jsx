import { render, screen, act, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { AuthProvider, useAuth } from '../../context/AuthContext'
import { makeToken, ADMIN_TOKEN, USER_TOKEN } from '../helpers'

vi.mock('../../api/auth', () => ({
  login: vi.fn(),
  register: vi.fn(),
  logout: vi.fn(),
}))

import { login as loginApi, register as registerApi, logout as logoutApi } from '../../api/auth'

function AuthDisplay() {
  const { isAuth, isAdmin, token } = useAuth()
  return (
    <div>
      <span data-testid="is-auth">{String(isAuth)}</span>
      <span data-testid="is-admin">{String(isAdmin)}</span>
      <span data-testid="token">{token ?? 'null'}</span>
    </div>
  )
}

function LoginButton() {
  const { login } = useAuth()
  return <button onClick={() => login('a@b.com', 'pass')}>Login</button>
}

function LogoutButton() {
  const { logout } = useAuth()
  return <button onClick={() => logout()}>Logout</button>
}

function RegisterButton() {
  const { register } = useAuth()
  return <button onClick={() => register('a@b.com', 'pass')}>Register</button>
}

function wrap(ui) {
  return render(
    <MemoryRouter>
      <AuthProvider>{ui}</AuthProvider>
    </MemoryRouter>
  )
}

describe('AuthContext', () => {
  describe('initial state', () => {
    it('isAuth is false when no token in localStorage', () => {
      wrap(<AuthDisplay />)
      expect(screen.getByTestId('is-auth')).toHaveTextContent('false')
    })

    it('isAuth is true when a token exists in localStorage', () => {
      localStorage.setItem('jwt_token', USER_TOKEN)
      wrap(<AuthDisplay />)
      expect(screen.getByTestId('is-auth')).toHaveTextContent('true')
    })

    it('isAdmin is false for a regular user token', () => {
      localStorage.setItem('jwt_token', USER_TOKEN)
      wrap(<AuthDisplay />)
      expect(screen.getByTestId('is-admin')).toHaveTextContent('false')
    })

    it('isAdmin is true for an admin token', () => {
      localStorage.setItem('jwt_token', ADMIN_TOKEN)
      wrap(<AuthDisplay />)
      expect(screen.getByTestId('is-admin')).toHaveTextContent('true')
    })

    it('isAdmin is false for a malformed token', () => {
      localStorage.setItem('jwt_token', 'not-a-jwt')
      wrap(<AuthDisplay />)
      expect(screen.getByTestId('is-admin')).toHaveTextContent('false')
    })
  })

  describe('login()', () => {
    it('stores the token from the API and sets isAuth to true', async () => {
      const user = userEvent.setup()
      loginApi.mockResolvedValueOnce({ data: { token: USER_TOKEN } })
      wrap(
        <>
          <AuthDisplay />
          <LoginButton />
        </>
      )
      await user.click(screen.getByRole('button', { name: /login/i }))
      await waitFor(() => expect(screen.getByTestId('is-auth')).toHaveTextContent('true'))
      expect(localStorage.getItem('jwt_token')).toBe(USER_TOKEN)
    })
  })

  describe('logout()', () => {
    it('clears the token and sets isAuth to false', async () => {
      localStorage.setItem('jwt_token', USER_TOKEN)
      logoutApi.mockResolvedValueOnce({})
      const user = userEvent.setup()
      wrap(
        <>
          <AuthDisplay />
          <LogoutButton />
        </>
      )
      expect(screen.getByTestId('is-auth')).toHaveTextContent('true')
      await user.click(screen.getByRole('button', { name: /logout/i }))
      await waitFor(() => expect(screen.getByTestId('is-auth')).toHaveTextContent('false'))
      expect(localStorage.getItem('jwt_token')).toBeNull()
    })

    it('succeeds even if the API call throws (fire-and-forget)', async () => {
      localStorage.setItem('jwt_token', USER_TOKEN)
      logoutApi.mockRejectedValueOnce(new Error('network error'))
      const user = userEvent.setup()
      wrap(
        <>
          <AuthDisplay />
          <LogoutButton />
        </>
      )
      await user.click(screen.getByRole('button', { name: /logout/i }))
      await waitFor(() => expect(screen.getByTestId('is-auth')).toHaveTextContent('false'))
    })
  })

  describe('register()', () => {
    it('calls register API then auto-logs-in', async () => {
      registerApi.mockResolvedValueOnce({})
      loginApi.mockResolvedValueOnce({ data: { token: USER_TOKEN } })
      const user = userEvent.setup()
      wrap(
        <>
          <AuthDisplay />
          <RegisterButton />
        </>
      )
      await user.click(screen.getByRole('button', { name: /register/i }))
      await waitFor(() => expect(screen.getByTestId('is-auth')).toHaveTextContent('true'))
      expect(registerApi).toHaveBeenCalledWith('a@b.com', 'pass')
      expect(loginApi).toHaveBeenCalledWith('a@b.com', 'pass')
    })
  })
})
