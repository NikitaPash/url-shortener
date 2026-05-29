import { useState, useEffect, useCallback } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Link2, ExternalLink, PlusCircle, ArrowRight, TrendingUp,
         BarChart2, QrCode, Pause, Play, Trash2 } from 'lucide-react'
import { QRCodeCanvas } from 'qrcode.react'
import { listLinks, shorten, toggleLink, deleteLink } from '../api/links'
import Card from '../components/ui/Card'
import Button from '../components/ui/Button'
import CopyButton from '../components/ui/CopyButton'
import Input from '../components/ui/Input'
import Spinner from '../components/ui/Spinner'

const RECENT_LIMIT = 5

export default function Dashboard() {
  const [links, setLinks] = useState([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)

  const [url, setUrl] = useState('')
  const [shortenLoading, setShortenLoading] = useState(false)
  const [created, setCreated] = useState(null)
  const [shortenError, setShortenError] = useState('')

  const [qrLink, setQrLink] = useState(null)
  const [busy, setBusy] = useState({})

  const navigate = useNavigate()

  const loadRecent = useCallback(() => {
    listLinks(RECENT_LIMIT, 0)
      .then(({ data }) => {
        setLinks(data.links ?? [])
        setTotal(data.total ?? 0)
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { loadRecent() }, [loadRecent])

  const setBusyFor = (id, value) =>
    setBusy((prev) => ({ ...prev, [id]: value }))

  async function handleShorten(e) {
    e.preventDefault()
    setShortenError('')
    setShortenLoading(true)
    try {
      const { data } = await shorten(url)
      setCreated(data)
      setUrl('')
      setTotal((t) => t + 1)
      // Prepend the new link and keep only RECENT_LIMIT entries.
      setLinks((prev) => [{ ...data, is_active: true }, ...prev].slice(0, RECENT_LIMIT))
    } catch (err) {
      setShortenError(err.response?.data?.error || 'Failed to shorten link')
    } finally {
      setShortenLoading(false)
    }
  }

  async function handleToggle(link) {
    setBusyFor(link.id, true)
    try {
      await toggleLink(link.id, !link.is_active)
      setLinks((prev) =>
        prev.map((l) => (l.id === link.id ? { ...l, is_active: !l.is_active } : l))
      )
    } finally {
      setBusyFor(link.id, false)
    }
  }

  async function handleDelete(link) {
    if (!window.confirm(`Permanently delete "${link.id}"? This cannot be undone.`)) return
    setBusyFor(link.id, true)
    try {
      await deleteLink(link.id)
      setLinks((prev) => prev.filter((l) => l.id !== link.id))
      setTotal((t) => t - 1)
    } finally {
      setBusyFor(link.id, false)
    }
  }

  function handleDownloadQR() {
    const canvas = document.getElementById('dash-qr-dl')
    if (!canvas || !qrLink) return
    const a = document.createElement('a')
    a.download = `qr-${qrLink.id}.png`
    a.href = canvas.toDataURL('image/png')
    a.click()
  }

  return (
    <div className="p-8 max-w-5xl mx-auto">
      <div className="mb-8">
        <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
        <p className="text-gray-500 text-sm mt-1">Overview of your links</p>
      </div>

      {/* Stat cards */}
      <div className="grid grid-cols-2 gap-6 mb-8">
        <Card className="p-6 flex items-center gap-4">
          <div className="bg-indigo-100 rounded-xl p-3">
            <Link2 className="text-indigo-600" size={24} />
          </div>
          <div>
            <p className="text-sm text-gray-500">Total Links</p>
            {loading ? <Spinner size="sm" /> : (
              <p className="text-3xl font-bold text-gray-900">{total}</p>
            )}
          </div>
        </Card>

        <Card className="p-6 flex items-center gap-4">
          <div className="bg-violet-100 rounded-xl p-3">
            <TrendingUp className="text-violet-600" size={24} />
          </div>
          <div>
            <p className="text-sm text-gray-500">Click Analytics</p>
            <Link
              to="/analytics"
              className="flex items-center gap-1 text-violet-600 font-semibold hover:underline text-sm mt-1"
            >
              Open Analytics <ArrowRight size={14} />
            </Link>
          </div>
        </Card>
      </div>

      {/* Quick shorten */}
      <Card className="p-6 mb-8">
        <h2 className="font-semibold text-gray-900 mb-4">Quick Shorten</h2>
        <form onSubmit={handleShorten} className="flex gap-3">
          <Input
            placeholder="https://example.com/your/very/long/url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            type="url"
            required
            className="flex-1"
          />
          <Button type="submit" loading={shortenLoading} className="shrink-0">
            <PlusCircle size={16} /> Shorten
          </Button>
        </form>
        {shortenError && <p className="text-sm text-red-600 mt-2">{shortenError}</p>}
        {created && (
          <div className="mt-4 flex items-center gap-3 bg-indigo-50 border border-indigo-200 rounded-lg px-4 py-3">
            <a
              href={created.short_url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-indigo-700 font-mono text-sm truncate flex-1 hover:underline"
            >
              {created.short_url}
            </a>
            <CopyButton text={created.short_url} />
          </div>
        )}
      </Card>

      {/* Recent links */}
      <Card>
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-100">
          <h2 className="font-semibold text-gray-900">Recent Links</h2>
          <Link
            to="/links"
            className="text-sm text-indigo-600 hover:underline flex items-center gap-1"
          >
            View all <ArrowRight size={14} />
          </Link>
        </div>

        {loading ? (
          <div className="py-12"><Spinner /></div>
        ) : links.length === 0 ? (
          <div className="py-12 text-center">
            <Link2 className="text-gray-300 mx-auto mb-3" size={40} />
            <p className="text-gray-500 text-sm">No links yet</p>
            <Link to="/links/new">
              <Button variant="secondary" className="mt-4">Create your first link</Button>
            </Link>
          </div>
        ) : (
          <div className="divide-y divide-gray-100">
            {links.map((link) => (
              <div
                key={link.id}
                className={`flex items-center gap-3 px-6 py-3 transition-colors hover:bg-gray-50 ${!link.is_active ? 'opacity-50' : ''}`}
              >
                {/* URL info */}
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <p className="text-sm font-mono text-indigo-600 truncate">{link.short_url}</p>
                    {!link.is_active && (
                      <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-gray-100 text-gray-500 uppercase tracking-wide shrink-0">
                        Paused
                      </span>
                    )}
                  </div>
                  <p className="text-xs text-gray-400 truncate">{link.original_url}</p>
                </div>

                {/* Actions — identical set to My Links */}
                <div className="flex items-center gap-1 shrink-0">
                  <CopyButton text={link.short_url} />
                  <button
                    onClick={() => setQrLink({ id: link.id, short_url: link.short_url })}
                    className="p-1 text-gray-400 hover:text-indigo-600 transition-colors"
                    title="Show QR code"
                  >
                    <QrCode size={14} />
                  </button>
                  <Link
                    to={`/links/${link.id}/analytics`}
                    className="p-1 text-gray-400 hover:text-indigo-600 transition-colors"
                    title="View analytics"
                  >
                    <BarChart2 size={14} />
                  </Link>
                  <button
                    onClick={() => handleToggle(link)}
                    disabled={busy[link.id]}
                    className={`p-1 transition-colors disabled:opacity-40 ${
                      link.is_active
                        ? 'text-gray-400 hover:text-amber-500'
                        : 'text-amber-500 hover:text-green-600'
                    }`}
                    title={link.is_active ? 'Deactivate link' : 'Activate link'}
                  >
                    {link.is_active ? <Pause size={14} /> : <Play size={14} />}
                  </button>
                  <a
                    href={link.original_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="p-1 text-gray-400 hover:text-gray-700 transition-colors"
                    title="Open original URL"
                  >
                    <ExternalLink size={14} />
                  </a>
                  <button
                    onClick={() => handleDelete(link)}
                    disabled={busy[link.id]}
                    className="p-1 text-gray-400 hover:text-red-500 transition-colors disabled:opacity-40"
                    title="Delete link permanently"
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>

      {/* QR Code Modal */}
      {qrLink && (
        <div
          className="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4"
          onClick={() => setQrLink(null)}
        >
          <div
            className="bg-white rounded-2xl p-8 flex flex-col items-center gap-5 shadow-2xl max-w-xs w-full"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 className="text-lg font-semibold text-gray-900">QR Code</h3>
            <QRCodeCanvas id="dash-qr-dl" value={qrLink.short_url} size={200} includeMargin />
            <p className="text-xs text-gray-400 font-mono break-all text-center">{qrLink.short_url}</p>
            <div className="flex gap-3 w-full">
              <Button className="flex-1" onClick={handleDownloadQR}>Download PNG</Button>
              <Button className="flex-1" variant="secondary" onClick={() => setQrLink(null)}>Close</Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
