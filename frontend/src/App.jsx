import { Routes, Route, Navigate } from 'react-router-dom'
import { AuthProvider, useAuth } from './context/AuthContext'
import AdminLayout from './components/Layout/AdminLayout'
import Landing from './pages/Landing'
import Login from './pages/Login'
import Register from './pages/Register'
import Dashboard from './pages/Dashboard'
import Links from './pages/Links'
import CreateLink from './pages/CreateLink'
import Analytics from './pages/Analytics'
import LinkAnalytics from './pages/LinkAnalytics'
import Admin from './pages/Admin'
import NotFound from './pages/NotFound'

function ProtectedLayout() {
  const { isAuth } = useAuth()
  if (!isAuth) return <Navigate to="/login" replace />
  return <AdminLayout />
}

function AdminRoute({ children }) {
  const { isAdmin } = useAuth()
  return isAdmin ? children : <Navigate to="/dashboard" replace />
}

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/" element={<Landing />} />
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />

        <Route element={<ProtectedLayout />}>
          <Route path="/dashboard" element={<Dashboard />} />
          <Route path="/links" element={<Links />} />
          <Route path="/links/new" element={<CreateLink />} />
          <Route path="/links/:id/analytics" element={<LinkAnalytics />} />
          <Route path="/analytics" element={<Analytics />} />
          <Route
            path="/admin"
            element={
              <AdminRoute>
                <Admin />
              </AdminRoute>
            }
          />
        </Route>

        <Route path="/not-found" element={<NotFound />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </AuthProvider>
  )
}
