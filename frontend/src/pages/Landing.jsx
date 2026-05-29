import { Link } from 'react-router-dom'
import { Zap, BarChart3, Brain, ArrowRight } from 'lucide-react'

const features = [
  {
    icon: Zap,
    title: 'Instant Redirects',
    desc: 'Redis-cached lookups deliver redirects in milliseconds. Cache misses fall through to PostgreSQL and lazily repopulate.',
  },
  {
    icon: BarChart3,
    title: 'Click Analytics',
    desc: 'Track every click by country, device, referrer, and time with seven pre-built Grafana dashboards backed by ClickHouse.',
  },
  {
    icon: Brain,
    title: 'AI Insights',
    desc: 'Ask questions in plain English — a Gemini-powered agent generates SQL, validates it, and queries ClickHouse for you.',
  },
]


export default function Landing() {
  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-900 via-slate-800 to-indigo-900 text-white">
      {/* Navbar */}
      <header className="flex items-center justify-between px-8 py-5 max-w-6xl mx-auto">
        <div className="flex items-center gap-2">
          <Zap className="text-indigo-400" size={24} />
          <span className="font-bold text-xl">shor.ty</span>
        </div>
        <div className="flex items-center gap-3">
          <Link
            to="/login"
            className="text-sm text-slate-300 hover:text-white transition-colors px-4 py-2"
          >
            Log In
          </Link>
          <Link
            to="/register"
            className="bg-indigo-600 hover:bg-indigo-500 text-sm font-medium px-5 py-2 rounded-lg transition-colors"
          >
            Get Started Free
          </Link>
        </div>
      </header>

      {/* Hero */}
      <section className="text-center py-24 px-4 max-w-4xl mx-auto">
        <div className="inline-flex items-center gap-2 bg-indigo-600/20 border border-indigo-500/30 rounded-full px-4 py-1.5 text-sm text-indigo-300 mb-6">
          <span className="relative flex h-2 w-2">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-indigo-400 opacity-75" />
            <span className="relative inline-flex rounded-full h-2 w-2 bg-indigo-500" />
          </span>
          Production-ready distributed URL shortener
        </div>
        <h1 className="text-6xl font-extrabold mb-6 leading-tight">
          Shorten. Share.{' '}
          <span className="text-transparent bg-clip-text bg-gradient-to-r from-indigo-400 to-violet-400">
            Track Everything.
          </span>
        </h1>
        <p className="text-lg text-slate-400 mb-10 max-w-2xl mx-auto">
          A high-performance URL shortener built on Go, Kafka, ClickHouse, and Redis — with
          AI-powered analytics and real-time Grafana dashboards.
        </p>
        <div className="flex items-center justify-center gap-4">
          <Link
            to="/register"
            className="flex items-center gap-2 bg-indigo-600 hover:bg-indigo-500 font-semibold px-8 py-3 rounded-xl transition-colors text-lg"
          >
            Start for Free <ArrowRight size={20} />
          </Link>
          <Link
            to="/login"
            className="font-medium px-8 py-3 rounded-xl border border-slate-600 hover:border-slate-400 transition-colors text-lg text-slate-300"
          >
            Log In
          </Link>
        </div>
      </section>

      {/* Feature cards */}
      <section className="max-w-6xl mx-auto px-8 pb-24 grid md:grid-cols-3 gap-6">
        {features.map(({ icon: Icon, title, desc }) => (
          <div
            key={title}
            className="bg-slate-800/60 border border-slate-700 rounded-2xl p-6 backdrop-blur"
          >
            <div className="bg-indigo-600/20 rounded-xl p-3 inline-flex mb-4">
              <Icon className="text-indigo-400" size={22} />
            </div>
            <h3 className="font-semibold text-lg mb-2">{title}</h3>
            <p className="text-slate-400 text-sm leading-relaxed">{desc}</p>
          </div>
        ))}
      </section>

    </div>
  )
}
