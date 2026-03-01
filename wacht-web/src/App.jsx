import { useEffect, useState } from 'react'
import { API_URL, getToken, getEmail, clearToken, clearEmail, authHeaders } from './api.js'
import LoginPage from './LoginPage.jsx'
import Dashboard from './Dashboard.jsx'
import AccountPage from './AccountPage.jsx'

export default function App() {
  const [token, setTokenState] = useState(getToken())
  const [email, setEmail] = useState(getEmail())
  const [isAdmin, setIsAdmin] = useState(false)
  const [meLoaded, setMeLoaded] = useState(false)
  const [page, setPage] = useState(() => new URLSearchParams(window.location.search).get('page') ?? 'dashboard')

  useEffect(() => {
    if (!token) return
    fetch(`${API_URL}/api/auth/me`, { headers: authHeaders() })
      .then(r => r.ok ? r.json() : null)
      .then(data => { if (data) setIsAdmin(data.is_admin) })
      .catch(() => {})
      .finally(() => setMeLoaded(true))
  }, [token])

  function handleLogin(userEmail) {
    setTokenState(getToken())
    setEmail(userEmail)
  }

  function handleLogout() {
    clearToken()
    clearEmail()
    const loginUrl = import.meta.env.VITE_LOGIN_URL ?? '/'
    window.location.href = loginUrl
  }

  if (!token) return <LoginPage onLogin={handleLogin} />
  if (!meLoaded) return null
  if (page === 'account') return <AccountPage email={email} isAdmin={isAdmin} onBack={() => setPage('dashboard')} onLogout={handleLogout} />
  return <Dashboard email={email} onLogout={handleLogout} onAccount={() => setPage('account')} />
}
