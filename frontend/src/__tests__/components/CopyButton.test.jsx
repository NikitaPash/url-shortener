import { render, screen, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import CopyButton from '../../components/ui/CopyButton'

describe('CopyButton', () => {
  it('renders "Copy" label initially', () => {
    render(<CopyButton text="https://shor.ty/abc" />)
    expect(screen.getByRole('button', { name: /copy/i })).toBeInTheDocument()
  })

  it('calls clipboard.writeText with the correct text on click', async () => {
    const user = userEvent.setup()
    render(<CopyButton text="https://shor.ty/abc123" />)
    await user.click(screen.getByRole('button'))
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('https://shor.ty/abc123')
  })

  it('shows "Copied!" immediately after clicking', async () => {
    const user = userEvent.setup()
    render(<CopyButton text="test" />)
    await user.click(screen.getByRole('button'))
    expect(screen.getByRole('button', { name: /copied/i })).toBeInTheDocument()
  })

  it('reverts to "Copy" after 2 seconds', async () => {
    vi.useFakeTimers()
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    render(<CopyButton text="test" />)
    await user.click(screen.getByRole('button'))
    expect(screen.getByRole('button', { name: /copied/i })).toBeInTheDocument()
    act(() => { vi.advanceTimersByTime(2001) })
    expect(screen.getByRole('button', { name: /^copy$/i })).toBeInTheDocument()
    vi.useRealTimers()
  })
})
