import { useState } from 'react'
import { API_URL, saveEmail, setToken } from './api.js'
import * as ui from './ui.js'

export default function SetupPasswordPage({ setupToken, onComplete, appName = 'Wacht' }) {
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [err, setErr] = useState(null)
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e) {
    e.preventDefault()
    setErr(null)
    if (password !== confirm) {
      setErr('Passwords do not match')
      return
    }
    setLoading(true)
    try {
      const res = await fetch(`${API_URL}/api/auth/setup-password`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token: setupToken, new_password: password }),
      })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text.trim() || `HTTP ${res.status}`)
      }
      const data = await res.json()
      setToken(data.token)
      saveEmail(data.email)
      onComplete(data.email)
    } catch (e) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gray-900 flex items-center justify-center p-6">
      <div className="w-full max-w-sm">
        <h1 className="text-xl font-bold text-gray-100 mb-6">{appName}</h1>
        <form onSubmit={handleSubmit} className={`${ui.card} p-6`}>
          <p className="mb-4 text-sm text-gray-400">Set your account password.</p>
          <div className="mb-4">
            <label className={ui.label}>New password</label>
            <input
              type="password"
              required
              value={password}
              onChange={e => setPassword(e.target.value)}
              className={ui.input}
            />
          </div>
          <div className="mb-4">
            <label className={ui.label}>Confirm password</label>
            <input
              type="password"
              required
              value={confirm}
              onChange={e => setConfirm(e.target.value)}
              className={ui.input}
            />
          </div>
          {err && <p className={`mb-3 ${ui.errorText}`}>{err}</p>}
          <button type="submit" disabled={loading} className={`w-full ${ui.btn.primaryMd}`}>
            {loading ? 'Saving…' : 'Set password'}
          </button>
        </form>
      </div>
    </div>
  )
}
