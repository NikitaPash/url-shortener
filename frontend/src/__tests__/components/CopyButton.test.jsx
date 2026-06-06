import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import CopyButton from '../../components/ui/CopyButton'

describe('CopyButton', () => {
  it('renders "Copy" label initially', () => {
    render(<CopyButton text="https://shor.ty/abc" />)
    expect(screen.getByRole('button', { name: /copy/i })).toBeInTheDocument()
  })

  it('calls clipboard.writeText with the correct text on click', () => {
    // Use fireEvent (not userEvent) here: userEvent.setup() swaps in its own
    // clipboard stub, which would defeat the spy assertion below.
    render(<CopyButton text="https://shor.ty/abc123" />)
    fireEvent.click(screen.getByRole('button'))
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('https://shor.ty/abc123')
  })

  it('shows "Copied!" immediately after clicking', async () => {
    const user = userEvent.setup()
    render(<CopyButton text="test" />)
    await user.click(screen.getByRole('button'))
    expect(screen.getByRole('button', { name: /copied/i })).toBeInTheDocument()
  })
})
