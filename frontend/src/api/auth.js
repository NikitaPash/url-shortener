import client from './client'

export const register = (email, password) =>
  client.post('/auth/register', { email, password })

export const login = (email, password) =>
  client.post('/auth/login', { email, password })

export const logout = () =>
  client.post('/auth/logout')
