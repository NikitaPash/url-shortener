import client from './client'

export const shorten = (url, customAlias = '') =>
  client.post('/api/shorten', {
    url,
    ...(customAlias ? { custom_alias: customAlias } : {}),
  })

export const listLinks = (limit = 20, offset = 0) =>
  client.get('/api/links', { params: { limit, offset } })

export const getLinkAnalytics = (id, params = {}) =>
  client.get(`/api/links/${id}/analytics`, { params })

export const toggleLink = (id, isActive) =>
  client.patch(`/api/links/${id}`, { is_active: isActive })

export const deleteLink = (id) =>
  client.delete(`/api/links/${id}`)
