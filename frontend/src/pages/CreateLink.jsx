import { useState } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, CheckCircle } from 'lucide-react'
import { shorten } from '../api/links'
import Card from '../components/ui/Card'
import Input from '../components/ui/Input'
import Button from '../components/ui/Button'
import CopyButton from '../components/ui/CopyButton'

export default function CreateLink() {
  const [url, setUrl] = useState('')
  const [customAlias, setCustomAlias] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState(null)

  const handleSubmit = async (e) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const { data } = await shorten(url, customAlias.trim())
      setResult(data)
    } catch (err) {
      setError(err.response?.data?.error || 'Failed to shorten URL')
    } finally {
      setLoading(false)
    }
  }

  const reset = () => {
    setResult(null)
    setUrl('')
    setCustomAlias('')
    setError('')
  }

  return (
    <div className="p-8 max-w-2xl mx-auto">
      <Link
        to="/links"
        className="flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700 mb-6 transition-colors"
      >
        <ArrowLeft size={16} /> Back to My Links
      </Link>

      <h1 className="text-2xl font-bold text-gray-900 mb-2">Create Short Link</h1>
      <p className="text-gray-500 text-sm mb-8">Shorten any URL</p>

      {result ? (
        <Card className="p-8 text-center">
          <div className="flex justify-center mb-4">
            <CheckCircle className="text-green-500" size={48} />
          </div>
          <h2 className="text-xl font-semibold text-gray-900 mb-2">Link Created!</h2>
          <div className="bg-indigo-50 border border-indigo-200 rounded-xl px-5 py-4 my-6 flex items-center gap-3">
            <a
              href={result.short_url}
              target="_blank"
              rel="noopener noreferrer"
              className="font-mono text-indigo-700 text-lg flex-1 text-left hover:underline truncate"
            >
              {result.short_url}
            </a>
            <CopyButton text={result.short_url} />
          </div>
          <p className="text-xs text-gray-400 mb-6 truncate">→ {result.original_url}</p>
          <div className="flex gap-3 justify-center">
            <Button onClick={reset}>Create Another</Button>
            <Link to="/links">
              <Button variant="secondary">View All Links</Button>
            </Link>
          </div>
        </Card>
      ) : (
        <Card className="p-8">
          <form onSubmit={handleSubmit} className="space-y-5">
            <Input
              label="Original URL"
              type="url"
              placeholder="https://example.com/very/long/path?with=params"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              required
            />
            <Input
              label="Custom alias (optional)"
              type="text"
              placeholder="my-brand  (3–32 chars, lowercase, hyphens allowed)"
              value={customAlias}
              onChange={(e) => setCustomAlias(e.target.value.toLowerCase())}
              maxLength={32}
            />
            {error && (
              <p className="text-sm text-red-600 bg-red-50 border border-red-200 rounded-lg px-3 py-2">
                {error}
              </p>
            )}
            <Button type="submit" className="w-full" loading={loading}>
              Shorten URL
            </Button>
          </form>
        </Card>
      )}
    </div>
  )
}
