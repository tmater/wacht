import { useEffect, useState } from 'react'
import { API_URL, authHeaders } from './api.js'
import * as ui from './ui.js'

export default function AccountPage({ email, isAdmin, onBack, onLogout }) {
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

        <div className={`${ui.card} p-6 max-w-sm mb-8`}>
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

        {isAdmin && <SignupRequests onLogout={onLogout} />}
      </div>
    </div>
  )
}

function SignupRequests({ onLogout }) {
  const [requests, setRequests] = useState([])
  const [approved, setApproved] = useState(null) // { email, tempPassword }
  const [err, setErr] = useState(null)

  async function load() {
    try {
      const res = await fetch(`${API_URL}/api/admin/signup-requests`, { headers: authHeaders() })
      if (res.status === 401) { onLogout(); return }
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setRequests(await res.json())
    } catch (e) {
      setErr(e.message)
    }
  }

  useEffect(() => { load() }, [])

  async function handleApprove(id) {
    try {
      const res = await fetch(`${API_URL}/api/admin/signup-requests/${id}/approve`, {
        method: 'POST',
        headers: authHeaders(),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      setApproved({ email: data.email, tempPassword: data.temp_password })
      load()
    } catch (e) {
      setErr(e.message)
    }
  }

  async function handleReject(id) {
    try {
      const res = await fetch(`${API_URL}/api/admin/signup-requests/${id}`, {
        method: 'DELETE',
        headers: authHeaders(),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      load()
    } catch (e) {
      setErr(e.message)
    }
  }

  function emailTemplate(email, tempPassword) {
    return `Subject: Your Wacht account is ready

Hi,

Your account has been approved.

Login: ${window.location.origin}
Email: ${email}
Temporary password: ${tempPassword}

Please change your password after signing in.`
  }

  return (
    <div>
      <h2 className={`mb-4 ${ui.sectionHeader}`}>Signup Requests</h2>

      {err && <p className={`mb-3 ${ui.errorText}`}>{err}</p>}

      {approved && (
        <div className={`${ui.card} p-4 mb-4`}>
          <p className={`mb-2 ${ui.successText}`}>Approved — {approved.email}</p>
          <pre className="text-xs text-gray-400 whitespace-pre-wrap mb-3 font-mono bg-gray-900 rounded p-3">
            {emailTemplate(approved.email, approved.tempPassword)}
          </pre>
          <div className="flex items-center gap-2">
            <button
              onClick={() => navigator.clipboard.writeText(emailTemplate(approved.email, approved.tempPassword))}
              className={ui.btn.primary}
            >
              Copy email
            </button>
            <button onClick={() => setApproved(null)} className={ui.btn.ghost}>Dismiss</button>
          </div>
        </div>
      )}

      {requests.length === 0 && !approved && (
        <p className="text-sm text-gray-500">No pending requests.</p>
      )}

      {requests.length > 0 && (
        <div className={`${ui.card} px-4 divide-y divide-gray-700`}>
          {requests.map(r => (
            <div key={r.id} className="flex items-center justify-between gap-4 py-2">
              <span className="text-sm text-gray-100 font-mono">{r.email}</span>
              <span className="text-xs text-gray-500 shrink-0">{new Date(r.requested_at).toLocaleString()}</span>
              <div className="flex items-center gap-2 shrink-0">
                <button onClick={() => handleApprove(r.id)} className={ui.btn.primary}>Approve</button>
                <button onClick={() => handleReject(r.id)} className={ui.btn.danger}>Reject</button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
