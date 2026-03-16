import { useEffect, useState } from 'react'
import { API_URL, getToken, getEmail, clearToken, clearEmail, authHeaders } from './api.js'
import LoginPage from './LoginPage.jsx'
import SetupPasswordPage from './SetupPasswordPage.jsx'
import Navbar from './Navbar.jsx'
import Dashboard from './Dashboard.jsx'
import AccountPage from './AccountPage.jsx'

export default function App({ appName = 'Wacht', navExtra = null, showProbes = true }) {
  const [token, setTokenState] = useState(getToken())
  const [email, setEmail] = useState(getEmail())
  const [isAdmin, setIsAdmin] = useState(false)
  const [meLoaded, setMeLoaded] = useState(false)
  const [page, setPage] = useState(() => new URLSearchParams(window.location.search).get('page') ?? 'dashboard')
  const setupToken = new URLSearchParams(window.location.search).get('setup_token')

  useEffect(() => {
    if (!token) return
    fetch(`${API_URL}/api/auth/me`, { headers: authHeaders() })
      .then(r => r.ok ? r.json() : null)
      .then(data => { if (data) setIsAdmin(data.is_admin) })
      .catch(() => {})
      .finally(() => setMeLoaded(true))
  }, [token])

  function handleLogin(userEmail) {
    window.history.replaceState({}, '', window.location.pathname)
    setTokenState(getToken())
    setEmail(userEmail)
    setMeLoaded(false)
  }

  function handleLogout() {
    clearToken()
    clearEmail()
    const loginUrl = import.meta.env.VITE_LOGIN_URL ?? '/'
    window.location.href = loginUrl
  }

  if (!token && setupToken) return <SetupPasswordPage setupToken={setupToken} onComplete={handleLogin} appName={appName} />
  if (!token) return <LoginPage onLogin={handleLogin} appName={appName} />
  if (!meLoaded) return null

  return (
    <div className="min-h-screen bg-gray-900">
      <Navbar email={email} page={page} onNavigate={setPage} onLogout={handleLogout} appName={appName} navExtra={navExtra} />
      {page === 'account'   && <AccountPage email={email} isAdmin={isAdmin} onLogout={handleLogout} />}
      {page === 'dashboard' && <Dashboard onLogout={handleLogout} showProbes={showProbes} />}
    </div>
  )
}
