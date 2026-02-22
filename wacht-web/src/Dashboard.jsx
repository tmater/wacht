import { useEffect, useState } from 'react'
import { API_URL, REFRESH_INTERVAL_MS, authHeaders } from './api.js'
import CheckForm from './CheckForm.jsx'
import CheckCard from './CheckCard.jsx'
import ProbeRow from './ProbeRow.jsx'

export default function Dashboard({ email, onLogout, onAccount }) {
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
                onClick={onAccount}
                className="text-xs text-gray-500 hover:text-gray-300"
              >
                Account
              </button>
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
