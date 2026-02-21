import { useEffect, useState } from 'react'

const API_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:8080'
const REFRESH_INTERVAL_MS = 30_000
const CHECK_TYPES = ['http', 'tcp', 'dns']

function getToken() { return localStorage.getItem('wacht_token') }
function setToken(t) { localStorage.setItem('wacht_token', t) }
function clearToken() { localStorage.removeItem('wacht_token') }
function getEmail() { return localStorage.getItem('wacht_email') }
function saveEmail(e) { localStorage.setItem('wacht_email', e) }
function clearEmail() { localStorage.removeItem('wacht_email') }

function authHeaders() {
  return { 'Authorization': `Bearer ${getToken()}`, 'Content-Type': 'application/json' }
}

// ---- LoginPage ---------------------------------------------------------------

function LoginPage({ onLogin }) {
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
        </form>
      </div>
    </div>
  )
}

// ---- CheckForm ---------------------------------------------------------------

function CheckForm({ initial, onSave, onCancel }) {
  const isNew = !initial
  const [id, setId] = useState(initial?.ID ?? '')
  const [type, setType] = useState(initial?.Type ?? 'http')
  const [target, setTarget] = useState(initial?.Target ?? '')
  const [webhook, setWebhook] = useState(initial?.Webhook ?? '')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState(null)

  async function handleSubmit(e) {
    e.preventDefault()
    setErr(null)
    setSaving(true)
    try {
      const body = JSON.stringify({ ID: id, Type: type, Target: target, Webhook: webhook })
      const res = isNew
        ? await fetch(`${API_URL}/api/checks`, { method: 'POST', headers: authHeaders(), body })
        : await fetch(`${API_URL}/api/checks/${initial.ID}`, { method: 'PUT', headers: authHeaders(), body })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text.trim() || `HTTP ${res.status}`)
      }
      onSave()
    } catch (e) {
      setErr(e.message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="rounded-lg border border-gray-600 bg-gray-750 p-4 mb-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <label className="block text-xs text-gray-400 mb-1">ID</label>
          <input
            required
            disabled={!isNew}
            value={id}
            onChange={e => setId(e.target.value)}
            placeholder="check-my-api"
            className="w-full rounded bg-gray-700 border border-gray-600 px-3 py-1.5 text-sm text-gray-100 placeholder-gray-500 disabled:opacity-50 focus:outline-none focus:border-gray-400"
          />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1">Type</label>
          <select
            value={type}
            onChange={e => setType(e.target.value)}
            className="w-full rounded bg-gray-700 border border-gray-600 px-3 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-gray-400"
          >
            {CHECK_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
          </select>
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1">Target</label>
          <input
            required
            value={target}
            onChange={e => setTarget(e.target.value)}
            placeholder="https://example.com"
            className="w-full rounded bg-gray-700 border border-gray-600 px-3 py-1.5 text-sm text-gray-100 placeholder-gray-500 focus:outline-none focus:border-gray-400"
          />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1">Webhook <span className="text-gray-600">(optional)</span></label>
          <input
            value={webhook}
            onChange={e => setWebhook(e.target.value)}
            placeholder="https://hooks.example.com/..."
            className="w-full rounded bg-gray-700 border border-gray-600 px-3 py-1.5 text-sm text-gray-100 placeholder-gray-500 focus:outline-none focus:border-gray-400"
          />
        </div>
      </div>
      {err && <p className="mt-2 text-xs text-red-400">{err}</p>}
      <div className="mt-3 flex gap-2">
        <button
          type="submit"
          disabled={saving}
          className="rounded bg-indigo-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
        >
          {saving ? 'Saving…' : isNew ? 'Add check' : 'Save'}
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="rounded bg-gray-700 px-3 py-1.5 text-xs font-semibold text-gray-300 hover:bg-gray-600"
        >
          Cancel
        </button>
      </div>
    </form>
  )
}

// ---- CheckCard ---------------------------------------------------------------

function CheckCard({ check, statusCheck, onEdit, onDelete }) {
  const up = statusCheck?.status === 'up'
  const hasStatus = !!statusCheck

  return (
    <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="font-mono text-sm font-semibold text-gray-100">{check.ID}</p>
          <p className="mt-0.5 text-xs text-gray-400 break-all">{check.Target}</p>
          <p className="mt-0.5 text-xs text-gray-600 uppercase">{check.Type}</p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {hasStatus && (
            <span className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${up ? 'bg-green-900 text-green-300' : 'bg-red-900 text-red-300'}`}>
              {up ? 'UP' : 'DOWN'}
            </span>
          )}
          <button onClick={onEdit} className="text-xs text-gray-500 hover:text-gray-300">Edit</button>
          <button onClick={onDelete} className="text-xs text-gray-500 hover:text-red-400">Delete</button>
        </div>
      </div>
      {statusCheck?.incident_since && (
        <p className="mt-2 text-xs text-red-400">
          Down since {new Date(statusCheck.incident_since).toLocaleString()}
        </p>
      )}
    </div>
  )
}

// ---- ProbeRow ----------------------------------------------------------------

function ProbeRow({ probe }) {
  return (
    <div className="flex items-center justify-between py-2">
      <p className="font-mono text-sm text-gray-300">{probe.probe_id}</p>
      <div className="flex items-center gap-3">
        <p className="text-xs text-gray-500">
          {new Date(probe.last_seen_at).toLocaleTimeString()}
        </p>
        <span className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${probe.online ? 'bg-green-900 text-green-300' : 'bg-red-900 text-red-300'}`}>
          {probe.online ? 'ONLINE' : 'OFFLINE'}
        </span>
      </div>
    </div>
  )
}

// ---- Dashboard ---------------------------------------------------------------

function Dashboard({ email, onLogout }) {
  const [checks, setChecks] = useState([])
  const [statuses, setStatuses] = useState([])
  const [probes, setProbes] = useState([])
  const [lastUpdated, setLastUpdated] = useState(null)
  const [error, setError] = useState(null)
  const [showAddForm, setShowAddForm] = useState(false)
  const [editingId, setEditingId] = useState(null)

  async function fetchAll() {
    try {
      const [statusRes, checksRes] = await Promise.all([
        fetch(`${API_URL}/status`),
        fetch(`${API_URL}/api/checks`, { headers: authHeaders() }),
      ])
      if (checksRes.status === 401) { onLogout(); return }
      if (!statusRes.ok) throw new Error(`status HTTP ${statusRes.status}`)
      if (!checksRes.ok) throw new Error(`checks HTTP ${checksRes.status}`)
      const statusData = await statusRes.json()
      const checksData = await checksRes.json()
      setStatuses(statusData.checks ?? [])
      setProbes(statusData.probes ?? [])
      setChecks(checksData ?? [])
      setLastUpdated(new Date())
      setError(null)
    } catch (e) {
      setError(e.message)
    }
  }

  useEffect(() => {
    fetchAll()
    const id = setInterval(fetchAll, REFRESH_INTERVAL_MS)
    return () => clearInterval(id)
  }, [])

  async function handleDelete(id) {
    if (!confirm(`Delete check "${id}"?`)) return
    try {
      const res = await fetch(`${API_URL}/api/checks/${id}`, { method: 'DELETE', headers: authHeaders() })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      fetchAll()
    } catch (e) {
      setError(e.message)
    }
  }

  async function handleLogoutClick() {
    await fetch(`${API_URL}/api/auth/logout`, { method: 'POST', headers: authHeaders() })
    onLogout()
  }

  function handleSaved() {
    setShowAddForm(false)
    setEditingId(null)
    fetchAll()
  }

  const statusByID = Object.fromEntries(statuses.map(s => [s.check_id, s]))
  const allUp = statuses.length > 0 && statuses.every(s => s.status === 'up')
  const downCount = statuses.filter(s => s.status !== 'up').length

  return (
    <div className="min-h-screen bg-gray-900 p-6">
      <div className="mx-auto max-w-3xl">

        {/* Header */}
        <div className="mb-6 flex items-center justify-between">
          <h1 className="text-xl font-bold text-gray-100">Wacht</h1>
          <div className="flex items-center gap-4">
            {lastUpdated && (
              <p className="text-xs text-gray-500">Updated {lastUpdated.toLocaleTimeString()}</p>
            )}
            <div className="flex items-center gap-2">
              <span className="text-xs text-gray-500">{email}</span>
              <button
                onClick={handleLogoutClick}
                className="text-xs text-gray-500 hover:text-gray-300"
              >
                Sign out
              </button>
            </div>
          </div>
        </div>

        {error && (
          <div className="mb-4 rounded-lg border border-red-800 bg-red-950 p-3 text-sm text-red-400">
            Could not reach server: {error}
          </div>
        )}

        {/* Summary bar */}
        {statuses.length > 0 && (
          <div className="mb-4 rounded-lg border border-gray-700 bg-gray-800 p-4">
            <p className="text-sm text-gray-400">
              {allUp
                ? <span className="font-semibold text-green-400">All checks passing</span>
                : <span className="font-semibold text-red-400">{downCount} check{downCount !== 1 ? 's' : ''} down</span>
              }
              <span className="text-gray-600"> · {statuses.length} total</span>
            </p>
          </div>
        )}

        {/* Checks section */}
        <div className="mb-8">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-xs font-semibold uppercase tracking-wider text-gray-500">Checks</h2>
            {!showAddForm && (
              <button
                onClick={() => { setShowAddForm(true); setEditingId(null) }}
                className="rounded bg-indigo-600 px-3 py-1 text-xs font-semibold text-white hover:bg-indigo-500"
              >
                + Add check
              </button>
            )}
          </div>

          {showAddForm && (
            <CheckForm onSave={handleSaved} onCancel={() => setShowAddForm(false)} />
          )}

          {editingId && (
            <CheckForm
              initial={checks.find(c => c.ID === editingId)}
              onSave={handleSaved}
              onCancel={() => setEditingId(null)}
            />
          )}

          {checks.length === 0 && !error && !showAddForm && (
            <p className="text-sm text-gray-500">No checks yet.</p>
          )}

          <div className="grid gap-3 sm:grid-cols-2">
            {checks.map(check => (
              <CheckCard
                key={check.ID}
                check={check}
                statusCheck={statusByID[check.ID]}
                onEdit={() => { setEditingId(check.ID); setShowAddForm(false) }}
                onDelete={() => handleDelete(check.ID)}
              />
            ))}
          </div>
        </div>

        {/* Probes section */}
        {probes.length > 0 && (
          <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
            <h2 className="mb-2 text-xs font-semibold uppercase tracking-wider text-gray-500">Probes</h2>
            <div className="divide-y divide-gray-700">
              {probes.map(probe => (
                <ProbeRow key={probe.probe_id} probe={probe} />
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

// ---- App ---------------------------------------------------------------------

export default function App() {
  const [token, setTokenState] = useState(getToken())
  const [email, setEmail] = useState(getEmail())

  function handleLogin(userEmail) {
    setTokenState(getToken())
    saveEmail(userEmail)
    setEmail(userEmail)
  }

  function handleLogout() {
    clearToken()
    clearEmail()
    setTokenState(null)
    setEmail(null)
  }

  if (!token) return <LoginPage onLogin={handleLogin} />
  return <Dashboard email={email} onLogout={handleLogout} />
}
