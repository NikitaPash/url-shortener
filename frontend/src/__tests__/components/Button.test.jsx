import { render, screen } from '@testing-library/react'
import Button from '../../components/ui/Button'

describe('Button', () => {
  it('renders children', () => {
    render(<Button>Click me</Button>)
    expect(screen.getByRole('button', { name: /click me/i })).toBeInTheDocument()
  })

  it('primary variant is the default', () => {
    render(<Button>Primary</Button>)
    expect(screen.getByRole('button')).toHaveClass('bg-indigo-600')
  })

  it('secondary variant applies correct styles', () => {
    render(<Button variant="secondary">Secondary</Button>)
    const btn = screen.getByRole('button')
    expect(btn).toHaveClass('bg-white')
    expect(btn).toHaveClass('border-gray-300')
  })

  it('ghost variant applies correct styles', () => {
    render(<Button variant="ghost">Ghost</Button>)
    expect(screen.getByRole('button')).toHaveClass('text-gray-600')
  })

  it('is disabled and shows spinner when loading', () => {
    render(<Button loading>Saving</Button>)
    const btn = screen.getByRole('button')
    expect(btn).toBeDisabled()
    // Spinner is an SVG with animate-spin class.
    expect(btn.querySelector('svg.animate-spin')).not.toBeNull()
  })

  it('is disabled when disabled prop is passed', () => {
    render(<Button disabled>Disabled</Button>)
    expect(screen.getByRole('button')).toBeDisabled()
  })

  it('forwards extra props to the underlying button element', () => {
    render(<Button data-testid="my-btn" type="submit">Submit</Button>)
    expect(screen.getByTestId('my-btn')).toHaveAttribute('type', 'submit')
  })
})
