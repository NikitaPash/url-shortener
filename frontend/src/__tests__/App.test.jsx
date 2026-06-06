import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import App from '../App'

// Mock API modules consumed by any rendered page.
vi.mock('../api/auth', () => ({
  login: vi.fn(),
  register: vi.fn(),
  logout: vi.fn(),
}))
vi.mock('../api/links', () => ({
  listLinks: vi.fn().mockResolvedValue({ data: { links: [], total: 0 } }),
  shorten: vi.fn(),
}))
vi.mock('../api/analytics', () => ({
  query: vi.fn(),
}))

function renderAt(path) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>
  )
}

describe('App routing', () => {
  it('renders the Landing page at /', () => {
    renderAt('/')
    // Landing page has the brand name and "Get Started" CTA.
    expect(screen.getByText(/shor\.ty/i)).toBeInTheDocument()
  })

  it('renders the Login page at /login', () => {
    renderAt('/login')
    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument()
  })

  it('renders the Register page at /register', () => {
    renderAt('/register')
    expect(screen.getByRole('button', { name: /create account/i })).toBeInTheDocument()
  })

  it('redirects unauthenticated user from /dashboard to /login', () => {
    renderAt('/dashboard')
    // After redirect the Login page should be visible.
    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument()
  })

  it('redirects unauthenticated user from /analytics to /login', () => {
    renderAt('/analytics')
    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument()
  })
})
