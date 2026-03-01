import { useState } from 'react'
import { API_URL, CHECK_TYPES, authHeaders } from './api.js'
import * as ui from './ui.js'

export default function CheckForm({ initial, onSave, onCancel, onDelete }) {
  const isNew = !initial
  const [id, setId] = useState(initial?.ID ?? '')
  const [type, setType] = useState(initial?.Type ?? 'http')
  const [target, setTarget] = useState(initial?.Target ?? '')
  const [webhook, setWebhook] = useState(initial?.Webhook ?? '')
  const [interval, setInterval] = useState(initial?.Interval ?? 30)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState(null)

  async function handleSubmit(e) {
    e.preventDefault()
    setErr(null)
    setSaving(true)
    try {
      const body = JSON.stringify({ ID: id, Type: type, Target: target, Webhook: webhook, Interval: parseInt(interval, 10) })
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
    <form onSubmit={handleSubmit} className={`${ui.card} p-4 mb-3`}>
      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <label className={ui.label}>ID</label>
          <input
            required
            disabled={!isNew}
            value={id}
            onChange={e => setId(e.target.value)}
            placeholder="check-my-api"
            className={ui.inputSm}
          />
        </div>
        <div>
          <label className={ui.label}>Type</label>
          <select value={type} onChange={e => setType(e.target.value)} className={ui.select}>
            {CHECK_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
          </select>
        </div>
        <div>
          <label className={ui.label}>Target</label>
          <input
            required
            value={target}
            onChange={e => setTarget(e.target.value)}
            placeholder="https://example.com"
            className={ui.inputSm}
          />
        </div>
        <div>
          <label className={ui.label}>Webhook <span className="text-gray-600">(optional)</span></label>
          <input
            value={webhook}
            onChange={e => setWebhook(e.target.value)}
            placeholder="https://hooks.example.com/..."
            className={ui.inputSm}
          />
        </div>
        <div>
          <label className={ui.label}>Interval <span className="text-gray-600">(seconds)</span></label>
          <input
            type="number"
            min="1"
            max="86400"
            value={interval}
            onChange={e => setInterval(e.target.value)}
            className={ui.inputSm}
          />
        </div>
      </div>
      {err && <p className={`mt-2 ${ui.errorText}`}>{err}</p>}
      <div className="mt-3 flex items-center gap-2">
        <button type="submit" disabled={saving} className={ui.btn.primary}>
          {saving ? 'Saving…' : isNew ? 'Add check' : 'Save'}
        </button>
        <button type="button" onClick={onCancel} className={ui.btn.secondary}>
          Cancel
        </button>
        {!isNew && onDelete && (
          <button type="button" onClick={onDelete} className={`ml-auto ${ui.btn.danger}`}>
            Delete check
          </button>
        )}
      </div>
    </form>
  )
}
