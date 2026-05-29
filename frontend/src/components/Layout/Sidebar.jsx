import { NavLink, Link, useNavigate } from 'react-router-dom'
import { Link2, LayoutDashboard, BarChart3, Settings, LogOut, Zap, PlusCircle } from 'lucide-react'
import { useAuth } from '../../context/AuthContext'

const nav = [
  { to: '/dashboard', icon: LayoutDashboard, label: 'Dashboard' },
  { to: '/links', icon: Link2, label: 'My Links', end: true },
  { to: '/links/new', icon: PlusCircle, label: 'Create Link' },
  { to: '/analytics', icon: BarChart3, label: 'Analytics' },
  { to: '/admin', icon: Settings, label: 'Admin Panel', adminOnly: true },
]

export default function Sidebar() {
  const { logout, isAdmin } = useAuth()
  const navigate = useNavigate()
  const visibleNav = nav.filter((item) => !item.adminOnly || isAdmin)

  const handleLogout = async () => {
    await logout()
    navigate('/login')
  }

  return (
    <aside className="sticky top-0 h-screen flex flex-col w-64 bg-slate-900 shrink-0 overflow-y-auto">
      {/* Logo — links to the app home */}
      <Link
        to="/dashboard"
        className="flex items-center gap-2 px-6 py-5 border-b border-slate-700 hover:bg-slate-800 transition-colors"
      >
        <Zap className="text-indigo-400" size={22} />
        <span className="text-white font-bold text-lg tracking-tight">shor.ty</span>
      </Link>

      <nav className="flex-1 px-3 py-4 space-y-1">
        {visibleNav.map(({ to, icon: Icon, label, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              `flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors ${
                isActive
                  ? 'bg-indigo-600 text-white'
                  : 'text-slate-400 hover:bg-slate-800 hover:text-white'
              }`
            }
          >
            <Icon size={18} />
            {label}
          </NavLink>
        ))}
      </nav>

      {/* Logout — always visible at the bottom of the sticky sidebar */}
      <div className="px-3 py-4 border-t border-slate-700 shrink-0">
        <button
          onClick={handleLogout}
          className="flex items-center gap-3 px-3 py-2.5 w-full rounded-lg text-sm font-medium text-slate-400 hover:bg-slate-800 hover:text-white transition-colors"
        >
          <LogOut size={18} />
          Log Out
        </button>
      </div>
    </aside>
  )
}
