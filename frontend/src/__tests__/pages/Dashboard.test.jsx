import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders, USER_TOKEN } from '../helpers'
import Dashboard from '../../pages/Dashboard'

vi.mock('../../api/links', () => ({
  listLinks: vi.fn(),
  shorten: vi.fn(),
}))

// useNavigate is called inside Link components — keep it working.
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal()
  return { ...actual, useNavigate: () => vi.fn() }
})

import { listLinks, shorten } from '../../api/links'

const LINKS = [
  { id: 'a1', short_url: 'http://short.ly/a1', original_url: 'https://example.com/a' },
  { id: 'b2', short_url: 'http://short.ly/b2', original_url: 'https://example.com/b' },
]

describe('Dashboard page', () => {
  it('shows a spinner while links are loading', () => {
    listLinks.mockImplementationOnce(() => new Promise(() => {}))
    renderWithProviders(<Dashboard />, { token: USER_TOKEN })
    // The spinner is inside the stat card — multiple may exist.
    expect(document.querySelector('svg.animate-spin')).not.toBeNull()
  })

  it('displays total link count after data loads', async () => {
    listLinks.mockResolvedValueOnce({ data: { links: LINKS, total: 42 } })
    renderWithProviders(<Dashboard />, { token: USER_TOKEN })
    await waitFor(() => expect(screen.getByText('42')).toBeInTheDocument())
  })

  it('renders the list of recent links', async () => {
    listLinks.mockResolvedValueOnce({ data: { links: LINKS, total: 2 } })
    renderWithProviders(<Dashboard />, { token: USER_TOKEN })
    await waitFor(() => expect(screen.getByText('http://short.ly/a1')).toBeInTheDocument())
    expect(screen.getByText('http://short.ly/b2')).toBeInTheDocument()
  })

  it('shows "No links yet" when there are no links', async () => {
    listLinks.mockResolvedValueOnce({ data: { links: [], total: 0 } })
    renderWithProviders(<Dashboard />, { token: USER_TOKEN })
    await waitFor(() => expect(screen.getByText(/no links yet/i)).toBeInTheDocument())
  })

  it('renders the Quick Shorten form', async () => {
    listLinks.mockResolvedValueOnce({ data: { links: [], total: 0 } })
    renderWithProviders(<Dashboard />, { token: USER_TOKEN })
    await waitFor(() => screen.getByText(/quick shorten/i))
    expect(screen.getByPlaceholderText(/https:\/\/example\.com/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /shorten/i })).toBeInTheDocument()
  })

  it('shows the created short URL after successful shortening', async () => {
    listLinks.mockResolvedValueOnce({ data: { links: [], total: 0 } })
    shorten.mockResolvedValueOnce({ data: { short_url: 'http://short.ly/new99' } })
    const user = userEvent.setup()
    renderWithProviders(<Dashboard />, { token: USER_TOKEN })
    await waitFor(() => screen.getByPlaceholderText(/https:\/\/example\.com/i))
    await user.type(screen.getByPlaceholderText(/https:\/\/example\.com/i), 'https://example.com/my-long-url')
    await user.click(screen.getByRole('button', { name: /shorten/i }))
    await waitFor(() =>
      expect(screen.getByText('http://short.ly/new99')).toBeInTheDocument()
    )
  })

  it('shows an error message when shortening fails', async () => {
    listLinks.mockResolvedValueOnce({ data: { links: [], total: 0 } })
    shorten.mockRejectedValueOnce({
      response: { data: { error: 'invalid URL' } },
    })
    const user = userEvent.setup()
    renderWithProviders(<Dashboard />, { token: USER_TOKEN })
    await waitFor(() => screen.getByPlaceholderText(/https:\/\/example\.com/i))
    await user.type(screen.getByPlaceholderText(/https:\/\/example\.com/i), 'not-a-url')
    await user.click(screen.getByRole('button', { name: /shorten/i }))
    await waitFor(() => expect(screen.getByText(/invalid url/i)).toBeInTheDocument())
  })
})
