import { useState } from 'react'
import { API_URL, authHeaders } from './api.js'
import * as ui from './ui.js'

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
    <div className={ui.page}>
      <div className="mx-auto max-w-3xl">
        <div className="mb-6 flex items-center justify-between">
          <h1 className="text-xl font-bold text-gray-100">Wacht</h1>
          <button onClick={onBack} className={ui.btn.ghost}>← Back</button>
        </div>

        <h2 className={`mb-4 ${ui.sectionHeader}`}>Account</h2>

        <div className={`${ui.card} p-6 max-w-sm`}>
          <p className="mb-4 text-xs text-gray-400">{email}</p>

          <form onSubmit={handleSubmit}>
            <div className="mb-3">
              <label className={ui.label}>Current password</label>
              <input type="password" required value={current} onChange={e => setCurrent(e.target.value)} className={ui.input} />
            </div>
            <div className="mb-3">
              <label className={ui.label}>New password</label>
              <input type="password" required value={next} onChange={e => setNext(e.target.value)} className={ui.input} />
            </div>
            <div className="mb-4">
              <label className={ui.label}>Confirm new password</label>
              <input type="password" required value={confirm} onChange={e => setConfirm(e.target.value)} className={ui.input} />
            </div>
            {err && <p className={`mb-3 ${ui.errorText}`}>{err}</p>}
            {success && <p className={`mb-3 ${ui.successText}`}>Password updated.</p>}
            <button type="submit" disabled={saving} className={ui.btn.primaryMd}>
              {saving ? 'Saving…' : 'Update password'}
            </button>
          </form>
        </div>
      </div>
    </div>
  )
}
