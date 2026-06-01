import '@testing-library/jest-dom'

// jsdom has no clipboard API. We install a stubbed one, but it MUST be
// `configurable: true`: @testing-library/user-event's `userEvent.setup()`
// installs its own clipboard stub via Object.defineProperty, and with a
// non-configurable property that throws "Cannot redefine property: clipboard"
// (which also corrupts the module/mocks for the rest of the test file).
function installClipboardStub() {
  Object.defineProperty(navigator, 'clipboard', {
    value: {
      writeText: vi.fn().mockResolvedValue(undefined),
      readText: vi.fn().mockResolvedValue(''),
    },
    writable: true,
    configurable: true,
  })
}

// Reset clipboard + localStorage before every test to prevent state leaking
// (user-event may have swapped in its own clipboard stub during a prior test).
beforeEach(() => {
  localStorage.clear()
  installClipboardStub()
  vi.clearAllMocks()
})
