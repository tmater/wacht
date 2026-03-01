import { useState } from 'react'
import { API_URL, setToken, saveEmail } from './api.js'
import * as ui from './ui.js'

export default function LoginPage({ onLogin }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState(null)
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e) {
    e.preventDefault()
    setErr(null)
    setLoading(true)
    try {
      const res = await fetch(`${API_URL}/api/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text.trim() || `HTTP ${res.status}`)
      }
      const data = await res.json()
      setToken(data.token)
      saveEmail(data.email)
      onLogin(data.email)
    } catch (e) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gray-900 flex items-center justify-center p-6">
      <div className="w-full max-w-sm">
        <h1 className="text-xl font-bold text-gray-100 mb-6">Wacht</h1>
        <form onSubmit={handleSubmit} className={`${ui.card} p-6`}>
          <div className="mb-4">
            <label className={ui.label}>Email</label>
            <input
              type="email"
              required
              value={email}
              onChange={e => setEmail(e.target.value)}
              className={ui.input}
            />
          </div>
          <div className="mb-4">
            <label className={ui.label}>Password</label>
            <input
              type="password"
              required
              value={password}
              onChange={e => setPassword(e.target.value)}
              className={ui.input}
            />
          </div>
          {err && <p className={`mb-3 ${ui.errorText}`}>{err}</p>}
          <button type="submit" disabled={loading} className={`w-full ${ui.btn.primaryMd}`}>
            {loading ? 'Signing in…' : 'Sign in'}
          </button>
          <p className="mt-4 text-xs text-gray-500 text-center">
            No account?{' '}
            <a href="mailto:wacht.eu@proton.me" className="text-gray-400 hover:text-gray-300">
              Contact us for trial access
            </a>
          </p>
        </form>
      </div>
    </div>
  )
}
