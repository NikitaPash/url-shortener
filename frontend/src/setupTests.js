import '@testing-library/jest-dom'

// Mock clipboard API — not available in jsdom.
Object.defineProperty(navigator, 'clipboard', {
  value: { writeText: vi.fn().mockResolvedValue(undefined) },
  writable: true,
})

// Reset localStorage before every test to prevent state leaking.
beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
})
