import { Link } from 'react-router-dom'
import { Home, ArrowLeft } from 'lucide-react'
import Button from '../components/ui/Button'

export default function NotFound() {
  return (
    <div className="min-h-screen flex flex-col items-center justify-center bg-gray-50 p-8">
      <div className="text-center max-w-md">
        {/* Stacked 404 with broken-link visual */}
        <div className="relative mb-10 select-none">
          <p className="text-[10rem] font-black leading-none text-indigo-100 tracking-tighter">
            404
          </p>
          <div className="absolute inset-0 flex items-center justify-center gap-3">
            <span className="text-4xl">🔗</span>
            <span className="text-4xl opacity-30 line-through decoration-red-400">✗</span>
          </div>
        </div>

        <h1 className="text-2xl font-bold text-gray-900 mb-3">
          Link not found
        </h1>
        <p className="text-gray-500 mb-8 leading-relaxed">
          This link doesn't exist, has expired, or was deactivated by its owner.
          <br />
          Double-check the URL or contact whoever shared it with you.
        </p>

        <div className="flex flex-col sm:flex-row items-center justify-center gap-3">
          <a href="/app/">
            <Button>
              <Home size={15} /> Go to homepage
            </Button>
          </a>
          <button
            onClick={() => window.history.back()}
            className="inline-flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium text-gray-600 hover:bg-gray-100 transition-colors"
          >
            <ArrowLeft size={15} /> Go back
          </button>
        </div>
      </div>
    </div>
  )
}
