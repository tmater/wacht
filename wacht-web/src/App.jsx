import { useState } from 'react'
import { getToken, getEmail, clearToken, clearEmail } from './api.js'
import LoginPage from './LoginPage.jsx'
import Dashboard from './Dashboard.jsx'
import AccountPage from './AccountPage.jsx'

export default function App() {
  const [token, setTokenState] = useState(getToken())
  const [email, setEmail] = useState(getEmail())
  const [page, setPage] = useState('dashboard')

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
  if (page === 'account') return <AccountPage email={email} onBack={() => setPage('dashboard')} onLogout={handleLogout} />
  return <Dashboard email={email} onLogout={handleLogout} onAccount={() => setPage('account')} />
}
