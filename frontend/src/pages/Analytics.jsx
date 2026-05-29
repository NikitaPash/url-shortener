import { useState } from 'react'
import { Send, Brain } from 'lucide-react'
import { query } from '../api/analytics'
import Card from '../components/ui/Card'
import Button from '../components/ui/Button'
import Spinner from '../components/ui/Spinner'

const examples = [
  'How many clicks did I get today?',
  'Which link got the most clicks this week?',
  'What countries are my visitors from?',
  'What percentage of my traffic is from mobile?',
  'What are my top 5 referrer sources?',
]

// Raw agent/SQL errors leak internals and confuse users, so we show one calm
// message and keep the real cause in the console for debugging.
const FRIENDLY_ERROR =
  "Sorry, I couldn't answer that one. Try rephrasing your question — for example, ask about your clicks, links, countries, or devices."

export default function Analytics() {
  const [question, setQuestion] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState(null)
  const [error, setError] = useState('')

  const runQuery = async (text) => {
    if (!text.trim()) return
    setError('')
    setResult(null)
    setLoading(true)
    try {
      const { data } = await query(text)
      if (data.error) {
        console.error('analytics query returned an error:', data.error)
        setError(FRIENDLY_ERROR)
      } else {
        setResult(data)
      }
    } catch (err) {
      console.error('analytics query failed:', err.response?.data ?? err)
      setError(FRIENDLY_ERROR)
    } finally {
      setLoading(false)
    }
  }

  const handleSubmit = () => runQuery(question)

  const handleExample = (ex) => {
    setQuestion(ex)
    runQuery(ex)
  }

  return (
    <div className="p-8 max-w-5xl mx-auto">
      <div className="mb-8">
        <h1 className="text-2xl font-bold text-gray-900">Analytics</h1>
        <p className="text-gray-500 text-sm mt-1">
          Ask questions about your link performance in plain English
        </p>
      </div>

      {/* Query input */}
      <Card className="p-6 mb-6">
        <div className="flex items-start gap-3">
          <div className="bg-indigo-100 rounded-xl p-2.5 mt-0.5 shrink-0">
            <Brain className="text-indigo-600" size={20} />
          </div>
          <div className="flex-1">
            <div className="flex gap-3">
              <textarea
                className="flex-1 border border-gray-300 rounded-lg px-3 py-2 text-sm resize-none focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                rows={2}
                placeholder="Ask anything about your clicks, links, countries, devices…"
                value={question}
                onChange={(e) => setQuestion(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault()
                    handleSubmit()
                  }
                }}
              />
              <Button onClick={handleSubmit} loading={loading} className="self-end shrink-0">
                <Send size={15} /> Ask
              </Button>
            </div>
            <div className="flex flex-wrap gap-2 mt-3">
              {examples.map((ex) => (
                <button
                  key={ex}
                  onClick={() => handleExample(ex)}
                  className="text-xs bg-gray-100 hover:bg-indigo-50 hover:text-indigo-700 text-gray-600 px-3 py-1.5 rounded-full transition-colors"
                >
                  {ex}
                </button>
              ))}
            </div>
          </div>
        </div>
      </Card>

      {/* Loading */}
      {loading && (
        <Card className="p-12">
          <Spinner />
          <p className="text-center text-sm text-gray-500 mt-4">
            Generating SQL and querying ClickHouse…
          </p>
        </Card>
      )}

      {/* Error */}
      {error && !loading && (
        <Card className="p-6 border-red-200 bg-red-50">
          <p className="text-red-700 text-sm">{error}</p>
        </Card>
      )}

      {/* Results */}
      {result && !loading && (
        <Card>
          {result.explanation && (
            <div className="px-6 py-4 border-b border-gray-100 bg-indigo-50/50">
              <p className="text-sm text-gray-700">{result.explanation}</p>
            </div>
          )}
          {result.data?.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-gray-50">
                  <tr>
                    {result.columns?.map((col) => (
                      <th
                        key={col}
                        className="text-left px-6 py-3 text-xs font-medium text-gray-500 uppercase tracking-wide"
                      >
                        {col}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {result.data.map((row, i) => (
                    <tr key={i} className="hover:bg-gray-50">
                      {row.map((cell, j) => (
                        <td key={j} className="px-6 py-3 text-gray-700">
                          {String(cell)}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
              <p className="text-xs text-gray-400 px-6 py-3 border-t border-gray-100">
                {result.row_count} row{result.row_count !== 1 ? 's' : ''}
              </p>
            </div>
          ) : (
            <p className="text-center text-gray-400 text-sm py-8">No data returned</p>
          )}
        </Card>
      )}
    </div>
  )
}
