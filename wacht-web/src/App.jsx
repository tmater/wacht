import { useEffect, useState } from 'react'
import { API_URL, getToken, getEmail, clearToken, clearEmail, authHeaders } from './api.js'
import LoginPage from './LoginPage.jsx'
import SetupPasswordPage from './SetupPasswordPage.jsx'
import Navbar from './Navbar.jsx'
import Dashboard from './Dashboard.jsx'
import AccountPage from './AccountPage.jsx'
import PublicStatusPage from './PublicStatusPage.jsx'
import { stripBasePath } from './paths.js'

function publicStatusSlugFromPath(pathname) {
  const relativePath = stripBasePath(pathname)
  if (!relativePath.startsWith('/public/')) return ''
  const slug = relativePath.slice('/public/'.length).split('/')[0]
  return slug ? decodeURIComponent(slug) : ''
}

export default function App({ appName = 'Wacht', navExtra = null, showProbes = true }) {
  const [token, setTokenState] = useState(getToken())
  const [email, setEmail] = useState(getEmail())
  const [isAdmin, setIsAdmin] = useState(false)
  const [publicStatusSlug, setPublicStatusSlug] = useState('')
  const [meLoaded, setMeLoaded] = useState(false)
  const [page, setPage] = useState(() => new URLSearchParams(window.location.search).get('page') ?? 'dashboard')
  const setupToken = new URLSearchParams(window.location.search).get('setup_token')
  const publicPageSlug = publicStatusSlugFromPath(window.location.pathname)

  useEffect(() => {
    if (!token) return
    fetch(`${API_URL}/api/auth/me`, { headers: authHeaders() })
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (!data) return
        setIsAdmin(data.is_admin)
        setPublicStatusSlug(data.public_status_slug ?? '')
      })
      .catch(() => {})
      .finally(() => setMeLoaded(true))
  }, [token])

  function handleLogin(userEmail) {
    window.history.replaceState({}, '', window.location.pathname)
    setTokenState(getToken())
    setEmail(userEmail)
    setMeLoaded(false)
  }

  async function handleLogout() {
    try {
      if (getToken()) {
        await fetch(`${API_URL}/api/auth/logout`, {
          method: 'POST',
          headers: authHeaders(),
        })
      }
    } catch {
      // Local auth state should still be cleared even if the logout request fails.
    } finally {
      clearToken()
      clearEmail()
      setTokenState(null)
      setEmail(null)
      setIsAdmin(false)
      setMeLoaded(false)
      const loginUrl = import.meta.env.VITE_LOGIN_URL ?? '/'
      window.location.href = loginUrl
    }
  }

  if (publicPageSlug) return <PublicStatusPage slug={publicPageSlug} appName={appName} />
  if (!token && setupToken) return <SetupPasswordPage setupToken={setupToken} onComplete={handleLogin} appName={appName} />
  if (!token) return <LoginPage onLogin={handleLogin} appName={appName} />
  if (!meLoaded) return null

  return (
    <div className="min-h-screen bg-gray-900">
      <Navbar email={email} page={page} onNavigate={setPage} onLogout={handleLogout} appName={appName} navExtra={navExtra} />
      {page === 'account'   && <AccountPage email={email} isAdmin={isAdmin} onLogout={handleLogout} publicStatusSlug={publicStatusSlug} />}
      {page === 'dashboard' && <Dashboard onLogout={handleLogout} showProbes={showProbes} />}
    </div>
  )
}
