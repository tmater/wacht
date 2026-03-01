import { useState } from 'react'
import { API_URL, setToken, saveEmail } from './api.js'
import * as ui from './ui.js'

export default function LoginPage({ onLogin }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState(null)
  const [loading, setLoading] = useState(false)
  const [mode, setMode] = useState('login')       // 'login' | 'request'
  const [submitted, setSubmitted] = useState(false)

  async function handleSubmit(e) {
    e.preventDefault()
    setErr(null)
    setLoading(true)
    try {
      if (mode === 'login') {
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
      } else {
        const res = await fetch(`${API_URL}/api/auth/request-access`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email }),
        })
        if (!res.ok) {
          const text = await res.text()
          throw new Error(text.trim() || `HTTP ${res.status}`)
        }
        setSubmitted(true)
      }
    } catch (e) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }

  function switchMode(newMode) {
    setMode(newMode)
    setErr(null)
    setSubmitted(false)
  }

  return (
    <div className="min-h-screen bg-gray-900 flex items-center justify-center p-6">
      <div className="w-full max-w-sm">
        <h1 className="text-xl font-bold text-gray-100 mb-6">Wacht</h1>

        {submitted ? (
          <div className={`${ui.card} p-6`}>
            <p className={ui.successText}>Request received. You will be contacted when your account is approved.</p>
            <button type="button" onClick={() => switchMode('login')} className={`mt-4 ${ui.btn.ghost}`}>
              ← Back to sign in
            </button>
          </div>
        ) : (
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
            {mode === 'login' && (
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
            )}
            {err && <p className={`mb-3 ${ui.errorText}`}>{err}</p>}
            <button type="submit" disabled={loading} className={`w-full ${ui.btn.primaryMd}`}>
              {loading
                ? (mode === 'login' ? 'Signing in…' : 'Sending…')
                : (mode === 'login' ? 'Sign in' : 'Request access')}
            </button>
            <p className="mt-4 text-xs text-gray-500 text-center">
              {mode === 'login' ? (
                <>
                  No account?{' '}
                  <button type="button" onClick={() => switchMode('request')} className="text-gray-400 hover:text-gray-300">
                    Request access
                  </button>
                </>
              ) : (
                <button type="button" onClick={() => switchMode('login')} className="text-gray-400 hover:text-gray-300">
                  ← Back to sign in
                </button>
              )}
            </p>
          </form>
        )}
      </div>
    </div>
  )
}
