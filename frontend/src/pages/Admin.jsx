import { useState, useEffect } from 'react'
import { ExternalLink, CheckCircle, XCircle, BarChart3, Activity, Database } from 'lucide-react'
import Card from '../components/ui/Card'

const services = [
  {
    name: 'Grafana',
    description:
      'Seven pre-built dashboards: click volume, geo distribution, device breakdown, bot traffic, link leaderboard, top referrers, system health.',
    href: '/grafana/',
    icon: BarChart3,
    color: 'text-orange-500',
    bg: 'bg-orange-50',
  },
  {
    name: 'Jaeger',
    description:
      'Distributed tracing — visualise end-to-end request flows across Go API, ClickHouse consumer, and Python agent with W3C trace propagation.',
    href: '/jaeger/',
    icon: Activity,
    color: 'text-blue-500',
    bg: 'bg-blue-50',
  },
  {
    name: 'Prometheus',
    description:
      'Raw metrics — redirects, cache hits/misses, Kafka publishes, batch inserts. Scraped every 15 s from all three application services.',
    href: '/prometheus/',
    icon: Database,
    color: 'text-red-500',
    bg: 'bg-red-50',
  },
]


export default function Admin() {
  const [healthy, setHealthy] = useState(null)

  useEffect(() => {
    fetch('/healthz')
      .then((r) => setHealthy(r.ok))
      .catch(() => setHealthy(false))
  }, [])

  return (
    <div className="p-8 max-w-5xl mx-auto">
      <div className="mb-8">
        <h1 className="text-2xl font-bold text-gray-900">Admin Panel</h1>
        <p className="text-gray-500 text-sm mt-1">
          Observability services and system monitoring
        </p>
      </div>

      {/* Health check */}
      <Card className="p-5 mb-6 flex items-center gap-3">
        {healthy === null ? (
          <div className="h-4 w-4 rounded-full bg-gray-300 animate-pulse" />
        ) : healthy ? (
          <CheckCircle className="text-green-500" size={22} />
        ) : (
          <XCircle className="text-red-500" size={22} />
        )}
        <div>
          <p className="font-medium text-gray-900 text-sm">
            Go API —{' '}
            {healthy === null ? 'Checking…' : healthy ? 'Healthy' : 'Unreachable'}
          </p>
          <p className="text-xs text-gray-400">GET /healthz</p>
        </div>
      </Card>

      {/* Observability services */}
      <h2 className="text-lg font-semibold text-gray-900 mb-4">Observability Services</h2>
      <div className="grid sm:grid-cols-3 gap-4">
        {services.map(({ name, description, href, icon: Icon, color, bg }) => (
          <Card key={name} className="p-5 flex flex-col">
            <div className={`${bg} rounded-xl p-3 inline-flex mb-3 self-start`}>
              <Icon className={color} size={22} />
            </div>
            <h3 className="font-semibold text-gray-900 mb-1">{name}</h3>
            <p className="text-xs text-gray-500 flex-1 leading-relaxed mb-4">{description}</p>
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1.5 text-sm font-medium text-indigo-600 hover:text-indigo-800 transition-colors"
            >
              Open {name} <ExternalLink size={13} />
            </a>
          </Card>
        ))}
      </div>

    </div>
  )
}
