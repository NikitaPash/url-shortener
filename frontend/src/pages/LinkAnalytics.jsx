import { useState, useEffect } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, Link2, QrCode, Copy, Check, Download } from 'lucide-react'
import {
  AreaChart, Area, BarChart, Bar, PieChart, Pie, Cell,
  XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer,
} from 'recharts'
import { QRCodeCanvas } from 'qrcode.react'
import { getLinkAnalytics } from '../api/links'
import Card from '../components/ui/Card'
import Button from '../components/ui/Button'
import Spinner from '../components/ui/Spinner'

const PRESETS = [7, 30, 90]

// Per-category color maps — label-keyed so order in the API response doesn't matter.
const DEVICE_COLORS = {
  desktop: '#3B82F6',  // blue
  mobile:  '#10B981',  // emerald
  bot:     '#F97316',  // orange
  unknown: '#94A3B8',  // slate
}

const BROWSER_COLORS = {
  'Chrome':            '#4285F4',  // Google blue
  'Firefox':           '#FF6611',  // Firefox orange-red
  'Safari':            '#0CB0E8',  // Safari cyan
  'Edge':              '#0F7B0F',  // Edge green
  'Samsung Browser':   '#1428A0',  // Samsung navy
  'bot':               '#F97316',  // same as device bot
  'unknown':           '#94A3B8',  // slate
}
// Fallback palette for any browser not in the map above.
const BROWSER_FALLBACK = ['#A78BFA', '#F472B6', '#34D399', '#FBBF24', '#60A5FA', '#F87171']

// Use local calendar date so timezone differences don't shift the available range.
function isoDate(offsetDays) {
  const d = new Date()
  d.setDate(d.getDate() - offsetDays)
  return [
    d.getFullYear(),
    String(d.getMonth() + 1).padStart(2, '0'),
    String(d.getDate()).padStart(2, '0'),
  ].join('-')
}

// Browser-native ISO 3166-1 alpha-2 → English country name.
// Created once at module level; safe to share across renders.
const _regionNames = (() => {
  try { return new Intl.DisplayNames(['en'], { type: 'region' }) } catch { return null }
})()

function countryName(code) {
  if (!code || code === '(Unknown)') return code || '(Unknown)'
  try { return _regionNames?.of(code) || code } catch { return code }
}

export default function LinkAnalytics() {
  const { id } = useParams()
  const shortUrl = `${window.location.origin}/${id}`

  const [mode, setMode] = useState('preset')
  const [period, setPeriod] = useState(7)
  const [dateFrom, setDateFrom] = useState(() => isoDate(7))
  const [dateTo, setDateTo] = useState(() => isoDate(0))
  const [excludeBots, setExcludeBots] = useState(false)

  const [stats, setStats] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const [showQR, setShowQR] = useState(false)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (mode === 'custom' && (!dateFrom || !dateTo || dateFrom >= dateTo)) return

    setLoading(true)
    setError('')

    const params = { exclude_bots: excludeBots }
    if (mode === 'custom') {
      params.from = dateFrom
      params.to = dateTo
    } else {
      params.period = period
    }

    getLinkAnalytics(id, params)
      .then(({ data }) => setStats(data))
      .catch((err) => setError(err.response?.data?.error || 'Failed to load analytics'))
      .finally(() => setLoading(false))
  }, [id, period, dateFrom, dateTo, mode, excludeBots])

  function handleCopy() {
    navigator.clipboard.writeText(shortUrl).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  function handleDownloadQR() {
    const canvas = document.getElementById('qr-dl')
    if (!canvas) return
    const a = document.createElement('a')
    a.download = `qr-${id}.png`
    a.href = canvas.toDataURL('image/png')
    a.click()
  }

  function handleExportJSON() {
    if (!stats) return
    const blob = new Blob([JSON.stringify(stats, null, 2)], { type: 'application/json' })
    const a = document.createElement('a')
    a.download = `analytics-${id}.json`
    a.href = URL.createObjectURL(blob)
    a.click()
    URL.revokeObjectURL(a.href)
  }

  function handleExportCSV() {
    if (!stats?.clicks_over_time) return
    const header = 'date,clicks,previous_period\n'
    const rows = stats.clicks_over_time.map((d, i) =>
      `${d.date},${d.clicks},${stats.previous_period[i]?.clicks ?? 0}`
    ).join('\n')
    const blob = new Blob([header + rows], { type: 'text/csv' })
    const a = document.createElement('a')
    a.download = `analytics-${id}.csv`
    a.href = URL.createObjectURL(blob)
    a.click()
    URL.revokeObjectURL(a.href)
  }

  const timelineData = stats?.clicks_over_time?.map((item, i) => ({
    date: item.date,
    clicks: item.clicks,
    previous: stats.previous_period[i]?.clicks ?? 0,
  })) ?? []

  const today = isoDate(0)

  return (
    <div className="p-8 max-w-6xl mx-auto">
      <Link
        to="/links"
        className="flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700 mb-6 transition-colors"
      >
        <ArrowLeft size={16} /> Back to My Links
      </Link>

      {/* Title + action buttons */}
      <div className="flex items-start justify-between mb-6 gap-4 flex-wrap">
        <h1 className="text-2xl font-bold text-gray-900">
          Analytics for <span className="font-mono text-indigo-600">{id}</span>
        </h1>
        <div className="flex items-center gap-2 flex-wrap shrink-0">
          <Button variant="secondary" onClick={handleCopy}>
            {copied ? <Check size={14} className="text-green-600" /> : <Copy size={14} />}
            {copied ? 'Copied!' : 'Copy URL'}
          </Button>
          <Button variant="secondary" onClick={() => setShowQR(true)}>
            <QrCode size={14} /> QR Code
          </Button>
          {stats && (
            <>
              <Button variant="secondary" onClick={handleExportCSV}>
                <Download size={14} /> CSV
              </Button>
              <Button variant="secondary" onClick={handleExportJSON}>
                <Download size={14} /> JSON
              </Button>
            </>
          )}
        </div>
      </div>

      {/* Controls */}
      <div className="flex items-center justify-between mb-6 flex-wrap gap-3">
        {/* Bot toggle */}
        <label className="flex items-center gap-2.5 text-sm text-gray-600 cursor-pointer select-none">
          <button
            type="button"
            role="switch"
            aria-checked={excludeBots}
            onClick={() => setExcludeBots((v) => !v)}
            className={`relative w-9 h-5 rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-1 ${
              excludeBots ? 'bg-indigo-600' : 'bg-gray-300'
            }`}
          >
            <span
              className={`absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform duration-150 ${
                excludeBots ? 'translate-x-4' : ''
              }`}
            />
          </button>
          Exclude bots
        </label>

        {/* Period selector */}
        <div className="flex items-center gap-2 flex-wrap">
          {PRESETS.map((p) => (
            <Button
              key={p}
              variant={mode === 'preset' && period === p ? 'primary' : 'secondary'}
              onClick={() => { setMode('preset'); setPeriod(p) }}
            >
              {p}d
            </Button>
          ))}
          <Button
            variant={mode === 'custom' ? 'primary' : 'secondary'}
            onClick={() => setMode('custom')}
          >
            Custom
          </Button>
          {mode === 'custom' && (
            <div className="flex items-center gap-2">
              <input
                type="date"
                value={dateFrom}
                max={dateTo || today}
                onChange={(e) => setDateFrom(e.target.value)}
                className="border border-gray-300 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
              <span className="text-gray-400 text-sm">to</span>
              <input
                type="date"
                value={dateTo}
                min={dateFrom}
                max={today}
                onChange={(e) => setDateTo(e.target.value)}
                className="border border-gray-300 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
            </div>
          )}
        </div>
      </div>

      {/* Main content */}
      {loading ? (
        <div className="py-20"><Spinner /></div>
      ) : error ? (
        <Card className="p-12 text-center">
          <p className="text-red-600">{error}</p>
        </Card>
      ) : stats && stats.total_clicks === 0 ? (
        <Card className="p-16 text-center">
          <Link2 className="text-gray-300 mx-auto mb-3" size={48} />
          <p className="text-gray-500">No click data for this period. Share your link and check back soon.</p>
        </Card>
      ) : stats ? (
        <div className="space-y-6">

          {/* Stat cards */}
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
            {[
              { label: 'Total Clicks', value: stats.total_clicks.toLocaleString(), sub: excludeBots ? 'humans only' : 'all traffic' },
              { label: 'Unique Visitors', value: stats.unique_visitors.toLocaleString(), sub: 'by IP address' },
              { label: 'Bot Clicks', value: stats.bot_clicks.toLocaleString(), sub: excludeBots ? 'excluded from stats' : 'included in total' },
              { label: 'Avg / Day', value: stats.avg_per_day.toFixed(1), sub: 'clicks per day' },
            ].map(({ label, value, sub }) => (
              <Card key={label} className="p-5">
                <p className="text-xs font-medium text-gray-400 uppercase tracking-wide mb-1">{label}</p>
                <p className="text-3xl font-bold text-gray-900 tabular-nums">{value}</p>
                <p className="text-xs text-gray-400 mt-1">{sub}</p>
              </Card>
            ))}
          </div>

          {/* Clicks over time — full width */}
          <Card className="p-6">
            <h2 className="text-xs font-medium text-gray-400 uppercase tracking-wide mb-4">Clicks over time</h2>
            <ResponsiveContainer width="100%" height={230}>
              <AreaChart data={timelineData} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
                <XAxis dataKey="date" tickFormatter={(d) => d.slice(5)} tick={{ fontSize: 11 }} />
                <YAxis allowDecimals={false} tick={{ fontSize: 11 }} width={32} />
                <Tooltip />
                <Legend />
                <Area type="monotone" dataKey="previous" name="Previous period" stroke="#9CA3AF" fill="none" strokeDasharray="4 2" strokeWidth={1.5} dot={false} />
                <Area type="monotone" dataKey="clicks" name="This period" stroke="#4F46E5" fill="#EEF2FF" strokeWidth={2} dot={false} />
              </AreaChart>
            </ResponsiveContainer>
          </Card>

          {/* Devices + Browsers */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <Card className="p-6">
              <h2 className="text-xs font-medium text-gray-400 uppercase tracking-wide mb-4">Devices</h2>
              <ResponsiveContainer width="100%" height={220}>
                <PieChart>
                  <Pie
                    data={stats.devices}
                    dataKey="clicks"
                    nameKey="label"
                    cx="50%"
                    cy="42%"
                    innerRadius={48}
                    outerRadius={72}
                  >
                    {stats.devices.map((entry, i) => (
                      <Cell key={i} fill={DEVICE_COLORS[entry.label] ?? '#94A3B8'} />
                    ))}
                  </Pie>
                  <Tooltip />
                  <Legend />
                </PieChart>
              </ResponsiveContainer>
            </Card>

            <Card className="p-6">
              <h2 className="text-xs font-medium text-gray-400 uppercase tracking-wide mb-4">Browsers</h2>
              <ResponsiveContainer width="100%" height={220}>
                <PieChart>
                  <Pie
                    data={stats.browsers}
                    dataKey="clicks"
                    nameKey="label"
                    cx="50%"
                    cy="42%"
                    innerRadius={48}
                    outerRadius={72}
                  >
                    {stats.browsers.map((entry, i) => (
                      <Cell key={i} fill={BROWSER_COLORS[entry.label] ?? BROWSER_FALLBACK[i % BROWSER_FALLBACK.length]} />
                    ))}
                  </Pie>
                  <Tooltip />
                  <Legend />
                </PieChart>
              </ResponsiveContainer>
            </Card>
          </div>

          {/* Peak hours */}
          <Card className="p-6">
            <h2 className="text-xs font-medium text-gray-400 uppercase tracking-wide mb-4">Peak hours (UTC)</h2>
            <ResponsiveContainer width="100%" height={180}>
              <BarChart data={stats.peak_hours} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" vertical={false} />
                <XAxis dataKey="hour" tickFormatter={(h) => `${h}h`} tick={{ fontSize: 10 }} interval={1} />
                <YAxis allowDecimals={false} tick={{ fontSize: 11 }} width={32} />
                <Tooltip
                  labelFormatter={(h) => `${h}:00 – ${(parseInt(h) + 1) % 24}:00 UTC`}
                  formatter={(v) => [v, 'clicks']}
                />
                <Bar dataKey="clicks" fill="#4F46E5" radius={[2, 2, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </Card>

          {/* Top countries — ISO code on axis, full name on hover */}
          <Card className="p-6">
            <h2 className="text-xs font-medium text-gray-400 uppercase tracking-wide mb-4">Top countries</h2>
            <ResponsiveContainer width="100%" height={Math.max(180, stats.countries.length * 36)}>
              <BarChart data={stats.countries} layout="vertical" margin={{ top: 0, right: 8, left: 0, bottom: 0 }}>
                <XAxis type="number" allowDecimals={false} tick={{ fontSize: 11 }} />
                <YAxis
                  type="category"
                  dataKey="label"
                  width={32}
                  tick={{ fontSize: 11, fontWeight: 600 }}
                  tickFormatter={(code) => code === '(Unknown)' ? '?' : code}
                />
                <Tooltip labelFormatter={countryName} formatter={(v) => [v.toLocaleString(), 'clicks']} />
                <Bar dataKey="clicks" fill="#4F46E5" radius={[0, 4, 4, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </Card>

          {/* Top referrers */}
          <Card className="p-6">
            <h2 className="text-xs font-medium text-gray-400 uppercase tracking-wide mb-4">Top referrers</h2>
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-100">
                  <th className="text-left py-2 text-gray-400 font-medium text-xs uppercase tracking-wide">Source</th>
                  <th className="text-right py-2 text-gray-400 font-medium text-xs uppercase tracking-wide">Clicks</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-50">
                {stats.referrers.map((ref, i) => (
                  <tr key={i} className="hover:bg-gray-50">
                    <td className="py-2 text-gray-700 font-mono text-xs">
                      {ref.label.length > 60 ? ref.label.slice(0, 60) + '…' : ref.label}
                    </td>
                    <td className="py-2 text-right text-gray-900 font-medium tabular-nums">
                      {ref.clicks.toLocaleString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        </div>
      ) : null}

      {/* QR Code Modal */}
      {showQR && (
        <div
          className="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4"
          onClick={() => setShowQR(false)}
        >
          <div
            className="bg-white rounded-2xl p-8 flex flex-col items-center gap-5 shadow-2xl max-w-xs w-full"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 className="text-lg font-semibold text-gray-900">QR Code</h3>
            <QRCodeCanvas id="qr-dl" value={shortUrl} size={200} includeMargin />
            <p className="text-xs text-gray-400 font-mono break-all text-center">{shortUrl}</p>
            <div className="flex gap-3 w-full">
              <Button className="flex-1" onClick={handleDownloadQR}>Download PNG</Button>
              <Button className="flex-1" variant="secondary" onClick={() => setShowQR(false)}>Close</Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
