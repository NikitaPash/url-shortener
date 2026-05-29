import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { PlusCircle, ExternalLink, Link2, BarChart2, QrCode, Pause, Play, Trash2 } from 'lucide-react'
import { QRCodeCanvas } from 'qrcode.react'
import { listLinks, toggleLink, deleteLink } from '../api/links'
import Card from '../components/ui/Card'
import Button from '../components/ui/Button'
import CopyButton from '../components/ui/CopyButton'
import Spinner from '../components/ui/Spinner'

const LIMIT = 20

export default function Links() {
  const [links, setLinks] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [loading, setLoading] = useState(true)
  const [qrLink, setQrLink] = useState(null)
  const [busy, setBusy] = useState({}) // id → true when action in-flight

  const load = useCallback(() => {
    setLoading(true)
    listLinks(LIMIT, page * LIMIT)
      .then(({ data }) => {
        setLinks(data.links ?? [])
        setTotal(data.total ?? 0)
      })
      .finally(() => setLoading(false))
  }, [page])

  useEffect(() => { load() }, [load])

  const totalPages = Math.ceil(total / LIMIT)

  function setBusyFor(id, value) {
    setBusy((prev) => ({ ...prev, [id]: value }))
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
    const canvas = document.getElementById('links-qr-dl')
    if (!canvas || !qrLink) return
    const a = document.createElement('a')
    a.download = `qr-${qrLink.id}.png`
    a.href = canvas.toDataURL('image/png')
    a.click()
  }

  const fmt = (iso) =>
    new Date(iso).toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    })

  return (
    <div className="p-8 max-w-5xl mx-auto">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">My Links</h1>
          <p className="text-gray-500 text-sm mt-1">
            {total} link{total !== 1 ? 's' : ''} total
          </p>
        </div>
        <Link to="/links/new">
          <Button>
            <PlusCircle size={16} /> New Link
          </Button>
        </Link>
      </div>

      <Card>
        {loading ? (
          <div className="py-16"><Spinner /></div>
        ) : links.length === 0 ? (
          <div className="py-16 text-center">
            <Link2 className="text-gray-300 mx-auto mb-3" size={48} />
            <p className="text-gray-500 mb-4">You haven't created any links yet</p>
            <Link to="/links/new">
              <Button>Create your first link</Button>
            </Link>
          </div>
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-gray-50 border-b border-gray-200">
                  <tr>
                    <th className="text-left px-6 py-3 font-medium text-gray-500 uppercase tracking-wide text-xs">Short URL</th>
                    <th className="text-left px-6 py-3 font-medium text-gray-500 uppercase tracking-wide text-xs">Original URL</th>
                    <th className="text-left px-6 py-3 font-medium text-gray-500 uppercase tracking-wide text-xs">Created</th>
                    <th className="text-right px-6 py-3 font-medium text-gray-500 uppercase tracking-wide text-xs">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {links.map((link) => (
                    <tr
                      key={link.id}
                      className={`hover:bg-gray-50 transition-colors ${!link.is_active ? 'opacity-50' : ''}`}
                    >
                      <td className="px-6 py-3">
                        <div className="flex items-center gap-2">
                          <span className="font-mono text-indigo-600 text-xs">{link.short_url}</span>
                          {!link.is_active && (
                            <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-gray-100 text-gray-500 uppercase tracking-wide">
                              Paused
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-6 py-3 max-w-xs">
                        <span className="text-gray-600 truncate block text-xs">{link.original_url}</span>
                      </td>
                      <td className="px-6 py-3 text-gray-500 text-xs whitespace-nowrap">{fmt(link.created_at)}</td>
                      <td className="px-6 py-3">
                        <div className="flex items-center gap-1.5 justify-end">
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
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {totalPages > 1 && (
              <div className="flex items-center justify-between px-6 py-4 border-t border-gray-100">
                <span className="text-sm text-gray-500">
                  Showing {page * LIMIT + 1}–{Math.min((page + 1) * LIMIT, total)} of {total}
                </span>
                <div className="flex gap-2">
                  <Button variant="secondary" onClick={() => setPage((p) => p - 1)} disabled={page === 0}>Previous</Button>
                  <Button variant="secondary" onClick={() => setPage((p) => p + 1)} disabled={page >= totalPages - 1}>Next</Button>
                </div>
              </div>
            )}
          </>
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
            <QRCodeCanvas id="links-qr-dl" value={qrLink.short_url} size={200} includeMargin />
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
