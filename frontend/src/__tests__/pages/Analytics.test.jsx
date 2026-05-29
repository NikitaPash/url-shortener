import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders, USER_TOKEN } from '../helpers'
import Analytics from '../../pages/Analytics'

vi.mock('../../api/analytics', () => ({
  query: vi.fn(),
}))

import { query } from '../../api/analytics'

const RESULT = {
  explanation: 'You had 42 clicks today.',
  columns: ['total_clicks'],
  data: [[42]],
  row_count: 1,
}

describe('Analytics page', () => {
  it('renders the question textarea and Ask button', () => {
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    expect(screen.getByPlaceholderText(/ask anything/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /ask/i })).toBeInTheDocument()
  })

  it('renders example question chips', () => {
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    expect(screen.getByText(/how many clicks did I get today/i)).toBeInTheDocument()
    expect(screen.getByText(/what countries/i)).toBeInTheDocument()
  })

  it('clicking an example chip populates the textarea and fires the query', async () => {
    query.mockResolvedValueOnce({ data: RESULT })
    const user = userEvent.setup()
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    const chip = screen.getByText(/how many clicks did I get today/i)
    await user.click(chip)
    expect(screen.getByPlaceholderText(/ask anything/i)).toHaveValue(
      'How many clicks did I get today?'
    )
    await waitFor(() => expect(query).toHaveBeenCalledWith('How many clicks did I get today?'))
  })

  it('shows a spinner while the query is running', async () => {
    query.mockImplementationOnce(() => new Promise(() => {}))
    const user = userEvent.setup()
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    await user.type(screen.getByPlaceholderText(/ask anything/i), 'total clicks?')
    await user.click(screen.getByRole('button', { name: /ask/i }))
    expect(document.querySelector('svg.animate-spin')).not.toBeNull()
  })

  it('renders results table with explanation and data on success', async () => {
    query.mockResolvedValueOnce({ data: RESULT })
    const user = userEvent.setup()
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    await user.type(screen.getByPlaceholderText(/ask anything/i), 'how many clicks today?')
    await user.click(screen.getByRole('button', { name: /ask/i }))
    await waitFor(() => expect(screen.getByText('You had 42 clicks today.')).toBeInTheDocument())
    expect(screen.getByRole('columnheader', { name: /total_clicks/i })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: '42' })).toBeInTheDocument()
    expect(screen.getByText(/1 row/i)).toBeInTheDocument()
  })

  it('shows "No data returned" when the result has no rows', async () => {
    query.mockResolvedValueOnce({
      data: { explanation: 'Empty result.', columns: [], data: [], row_count: 0 },
    })
    const user = userEvent.setup()
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    await user.type(screen.getByPlaceholderText(/ask anything/i), 'empty?')
    await user.click(screen.getByRole('button', { name: /ask/i }))
    await waitFor(() => expect(screen.getByText(/no data returned/i)).toBeInTheDocument())
  })

  it('shows the friendly error message on API failure', async () => {
    query.mockRejectedValueOnce(new Error('500 Internal'))
    const user = userEvent.setup()
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    await user.type(screen.getByPlaceholderText(/ask anything/i), 'bad query')
    await user.click(screen.getByRole('button', { name: /ask/i }))
    await waitFor(() =>
      expect(screen.getByText(/sorry, i couldn't answer that one/i)).toBeInTheDocument()
    )
  })

  it('does not fire query when the textarea is empty', async () => {
    const user = userEvent.setup()
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    await user.click(screen.getByRole('button', { name: /ask/i }))
    expect(query).not.toHaveBeenCalled()
  })

  it('submits on Enter (without Shift)', async () => {
    query.mockResolvedValueOnce({ data: RESULT })
    const user = userEvent.setup()
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    const textarea = screen.getByPlaceholderText(/ask anything/i)
    await user.type(textarea, 'clicks today?')
    await user.keyboard('{Enter}')
    await waitFor(() => expect(query).toHaveBeenCalledWith('clicks today?'))
  })

  it('does NOT submit on Shift+Enter', async () => {
    const user = userEvent.setup()
    renderWithProviders(<Analytics />, { token: USER_TOKEN })
    const textarea = screen.getByPlaceholderText(/ask anything/i)
    await user.type(textarea, 'multiline')
    await user.keyboard('{Shift>}{Enter}{/Shift}')
    expect(query).not.toHaveBeenCalled()
  })
})
