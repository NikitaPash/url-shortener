import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { AuthProvider, useAuth } from '../../context/AuthContext'
import { ADMIN_TOKEN, USER_TOKEN } from '../helpers'

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
})
