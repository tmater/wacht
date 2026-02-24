import { useState } from 'react'
import { API_URL, CHECK_TYPES, authHeaders } from './api.js'

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
        <div>
          <label className="block text-xs text-gray-400 mb-1">Interval <span className="text-gray-600">(seconds)</span></label>
          <input
            type="number"
            min="1"
            max="86400"
            value={interval}
            onChange={e => setInterval(e.target.value)}
            className="w-full rounded bg-gray-700 border border-gray-600 px-3 py-1.5 text-sm text-gray-100 focus:outline-none focus:border-gray-400"
          />
        </div>
      </div>
      {err && <p className="mt-2 text-xs text-red-400">{err}</p>}
      <div className="mt-3 flex items-center gap-2">
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
        {!isNew && onDelete && (
          <button
            type="button"
            onClick={onDelete}
            className="ml-auto rounded bg-red-900 px-3 py-1.5 text-xs font-semibold text-red-300 hover:bg-red-800"
          >
            Delete check
          </button>
        )}
      </div>
    </form>
  )
}
