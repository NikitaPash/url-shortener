import { createContext, useContext, useState, useCallback } from 'react'
import { login as loginApi, register as registerApi, logout as logoutApi } from '../api/auth'

const AuthContext = createContext(null)

// Read the is_admin claim straight from the JWT payload so admin state survives
// a page reload without an extra round-trip. Returns false on any malformed token.
function isAdminToken(token) {
  if (!token) return false
  try {
    // JWT payloads are base64url; atob needs plain base64.
    const b64 = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')
    const payload = JSON.parse(atob(b64))
    return payload.is_admin === true
  } catch {
    return false
  }
}

export function AuthProvider({ children }) {
  const [token, setToken] = useState(() => localStorage.getItem('jwt_token'))

  const login = useCallback(async (email, password) => {
    const { data } = await loginApi(email, password)
    localStorage.setItem('jwt_token', data.token)
    setToken(data.token)
  }, [])

  const register = useCallback(
    async (email, password) => {
      await registerApi(email, password)
      await login(email, password)
    },
    [login]
  )

  const logout = useCallback(async () => {
    try {
      await logoutApi()
    } catch {}
    localStorage.removeItem('jwt_token')
    setToken(null)
  }, [])

  return (
    <AuthContext.Provider
      value={{ token, isAuth: !!token, isAdmin: isAdminToken(token), login, register, logout }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export const useAuth = () => useContext(AuthContext)
