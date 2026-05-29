import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Input from '../../components/ui/Input'

describe('Input', () => {
  it('renders a text input', () => {
    render(<Input />)
    expect(screen.getByRole('textbox')).toBeInTheDocument()
  })

  it('renders the label when provided', () => {
    render(<Input label="Email" />)
    expect(screen.getByText('Email')).toBeInTheDocument()
  })

  it('renders an error message when provided', () => {
    render(<Input error="This field is required" />)
    expect(screen.getByText('This field is required')).toBeInTheDocument()
  })

  it('calls onChange when the user types', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<Input onChange={onChange} />)
    await user.type(screen.getByRole('textbox'), 'hello')
    expect(onChange).toHaveBeenCalled()
  })

  it('reflects the value prop', () => {
    render(<Input value="prefilled" onChange={() => {}} />)
    expect(screen.getByRole('textbox')).toHaveValue('prefilled')
  })
})
