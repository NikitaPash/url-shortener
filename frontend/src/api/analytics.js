import client from './client'

export const query = (question) =>
  client.post('/api/query', { question })
