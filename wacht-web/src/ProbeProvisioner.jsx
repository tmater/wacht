import { useState } from 'react'
import { API_URL, authHeaders } from './api.js'
import * as ui from './ui.js'

function probeServerURL() {
  return new URL(API_URL || window.location.origin, window.location.href).origin
}

function configSnippet(probe) {
  return `server: ${probeServerURL()}
probe_id: ${probe.probe_id}
secret: ${probe.secret}
heartbeat_interval: 30s`
}

export default function ProbeProvisioner({ onCreated, onLogout }) {
  const [probeId, setProbeId] = useState('')
  const [created, setCreated] = useState(null)
  const [error, setError] = useState(null)
  const [saving, setSaving] = useState(false)

  async function handleSubmit(e) {
    e.preventDefault()
    setError(null)
    setSaving(true)
    try {
      const body = probeId.trim() ? { probe_id: probeId.trim() } : {}
      const res = await fetch(`${API_URL}/api/admin/probes`, {
        method: 'POST',
        headers: authHeaders(),
        body: JSON.stringify(body),
      })
      if (res.status === 401) { onLogout?.(); return }
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text.trim() || `HTTP ${res.status}`)
      }
      const data = await res.json()
      setCreated(data)
      setProbeId('')
      onCreated?.()
    } catch (e) {
      setError(e.message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mb-4">
      <form onSubmit={handleSubmit} className="flex flex-col gap-2 sm:flex-row sm:items-end">
        <div className="min-w-0 flex-1">
          <label className={ui.label}>Probe ID</label>
          <input
            value={probeId}
            onChange={e => setProbeId(e.target.value)}
            placeholder="probe-api-1"
            className={ui.inputSm}
          />
        </div>
        <button type="submit" disabled={saving} className={ui.btn.primary}>
          {saving ? 'Creating...' : 'Create probe'}
        </button>
      </form>

      {error && <p className={`mt-2 ${ui.errorText}`}>{error}</p>}

      {created && (
        <div className="mt-4 rounded border border-gray-700 bg-gray-900 p-3">
          <div className="mb-2 flex items-center justify-between gap-3">
            <p className={ui.successText}>Copy this reusable probe config now.</p>
            <button
              type="button"
              onClick={() => navigator.clipboard.writeText(configSnippet(created))}
              className={ui.btn.ghost}
            >
              Copy config
            </button>
          </div>
          <pre className="overflow-x-auto whitespace-pre-wrap break-all font-mono text-xs text-gray-300">
            {configSnippet(created)}
          </pre>
        </div>
      )}
    </div>
  )
}
