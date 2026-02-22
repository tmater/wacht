import { useState } from 'react'
import { API_URL, setToken, saveEmail } from './api.js'

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
        <form onSubmit={handleSubmit} className="rounded-lg border border-gray-700 bg-gray-800 p-6">
          <div className="mb-4">
            <label className="block text-xs text-gray-400 mb-1">Email</label>
            <input
              type="email"
              required
              value={email}
              onChange={e => setEmail(e.target.value)}
              className="w-full rounded bg-gray-700 border border-gray-600 px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-gray-400"
            />
          </div>
          <div className="mb-4">
            <label className="block text-xs text-gray-400 mb-1">Password</label>
            <input
              type="password"
              required
              value={password}
              onChange={e => setPassword(e.target.value)}
              className="w-full rounded bg-gray-700 border border-gray-600 px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-gray-400"
            />
          </div>
          {err && <p className="mb-3 text-xs text-red-400">{err}</p>}
          <button
            type="submit"
            disabled={loading}
            className="w-full rounded bg-indigo-600 px-3 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
          >
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
