import { useState } from 'react'
import { API_URL, authHeaders } from './api.js'

export default function AccountPage({ email, onBack, onLogout }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [err, setErr] = useState(null)
  const [success, setSuccess] = useState(false)
  const [saving, setSaving] = useState(false)

  async function handleSubmit(e) {
    e.preventDefault()
    setErr(null)
    setSuccess(false)
    if (next !== confirm) {
      setErr('New passwords do not match')
      return
    }
    setSaving(true)
    try {
      const res = await fetch(`${API_URL}/api/auth/change-password`, {
        method: 'PUT',
        headers: authHeaders(),
        body: JSON.stringify({ current_password: current, new_password: next }),
      })
      if (res.status === 401) { onLogout(); return }
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text.trim() || `HTTP ${res.status}`)
      }
      setCurrent('')
      setNext('')
      setConfirm('')
      setSuccess(true)
    } catch (e) {
      setErr(e.message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="min-h-screen bg-gray-900 p-6">
      <div className="mx-auto max-w-3xl">
        <div className="mb-6 flex items-center justify-between">
          <h1 className="text-xl font-bold text-gray-100">Wacht</h1>
          <button onClick={onBack} className="text-xs text-gray-500 hover:text-gray-300">← Back</button>
        </div>

        <h2 className="mb-4 text-xs font-semibold uppercase tracking-wider text-gray-500">Account</h2>

        <div className="rounded-lg border border-gray-700 bg-gray-800 p-6 max-w-sm">
          <p className="mb-4 text-xs text-gray-400">{email}</p>

          <form onSubmit={handleSubmit}>
            <div className="mb-3">
              <label className="block text-xs text-gray-400 mb-1">Current password</label>
              <input
                type="password"
                required
                value={current}
                onChange={e => setCurrent(e.target.value)}
                className="w-full rounded bg-gray-700 border border-gray-600 px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-gray-400"
              />
            </div>
            <div className="mb-3">
              <label className="block text-xs text-gray-400 mb-1">New password</label>
              <input
                type="password"
                required
                value={next}
                onChange={e => setNext(e.target.value)}
                className="w-full rounded bg-gray-700 border border-gray-600 px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-gray-400"
              />
            </div>
            <div className="mb-4">
              <label className="block text-xs text-gray-400 mb-1">Confirm new password</label>
              <input
                type="password"
                required
                value={confirm}
                onChange={e => setConfirm(e.target.value)}
                className="w-full rounded bg-gray-700 border border-gray-600 px-3 py-2 text-sm text-gray-100 focus:outline-none focus:border-gray-400"
              />
            </div>
            {err && <p className="mb-3 text-xs text-red-400">{err}</p>}
            {success && <p className="mb-3 text-xs text-green-400">Password updated.</p>}
            <button
              type="submit"
              disabled={saving}
              className="rounded bg-indigo-600 px-3 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
            >
              {saving ? 'Saving…' : 'Update password'}
            </button>
          </form>
        </div>
      </div>
    </div>
  )
}
