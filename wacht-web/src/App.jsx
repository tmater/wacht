import { useEffect, useState } from 'react'

const API_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:8080'
const REFRESH_INTERVAL_MS = 30_000

function CheckCard({ check }) {
  const up = check.status === 'up'
  return (
    <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
      <div className="flex items-center justify-between">
        <div>
          <p className="font-mono text-sm font-semibold text-gray-100">{check.check_id}</p>
          <p className="mt-0.5 text-xs text-gray-400 break-all">{check.target}</p>
        </div>
        <span
          className={`ml-4 shrink-0 rounded-full px-2.5 py-0.5 text-xs font-semibold ${
            up ? 'bg-green-900 text-green-300' : 'bg-red-900 text-red-300'
          }`}
        >
          {up ? 'UP' : 'DOWN'}
        </span>
      </div>
      {check.incident_since && (
        <p className="mt-2 text-xs text-red-400">
          Down since {new Date(check.incident_since).toLocaleString()}
        </p>
      )}
    </div>
  )
}

function ProbeRow({ probe }) {
  return (
    <div className="flex items-center justify-between py-2">
      <p className="font-mono text-sm text-gray-300">{probe.probe_id}</p>
      <div className="flex items-center gap-3">
        <p className="text-xs text-gray-500">
          {new Date(probe.last_seen_at).toLocaleTimeString()}
        </p>
        <span
          className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${
            probe.online ? 'bg-green-900 text-green-300' : 'bg-red-900 text-red-300'
          }`}
        >
          {probe.online ? 'ONLINE' : 'OFFLINE'}
        </span>
      </div>
    </div>
  )
}

export default function App() {
  const [checks, setChecks] = useState([])
  const [probes, setProbes] = useState([])
  const [lastUpdated, setLastUpdated] = useState(null)
  const [error, setError] = useState(null)

  async function fetchStatus() {
    try {
      const res = await fetch(`${API_URL}/status`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      setChecks(data.checks ?? [])
      setProbes(data.probes ?? [])
      setLastUpdated(new Date())
      setError(null)
    } catch (e) {
      setError(e.message)
    }
  }

  useEffect(() => {
    fetchStatus()
    const id = setInterval(fetchStatus, REFRESH_INTERVAL_MS)
    return () => clearInterval(id)
  }, [])

  const allUp = checks.length > 0 && checks.every(c => c.status === 'up')
  const downCount = checks.filter(c => c.status !== 'up').length

  return (
    <div className="min-h-screen bg-gray-900 p-6">
      <div className="mx-auto max-w-3xl">
        <div className="mb-6 flex items-center justify-between">
          <h1 className="text-xl font-bold text-gray-100">Wacht</h1>
          {lastUpdated && (
            <p className="text-xs text-gray-500">
              Updated {lastUpdated.toLocaleTimeString()}
            </p>
          )}
        </div>

        {error && (
          <div className="mb-4 rounded-lg border border-red-800 bg-red-950 p-3 text-sm text-red-400">
            Could not reach server: {error}
          </div>
        )}

        {checks.length === 0 && !error && (
          <p className="text-sm text-gray-500">Loading...</p>
        )}

        {checks.length > 0 && (
          <>
            <div className="mb-4 rounded-lg border border-gray-700 bg-gray-800 p-4">
              <p className="text-sm text-gray-400">
                {allUp ? (
                  <span className="font-semibold text-green-400">All checks passing</span>
                ) : (
                  <span className="font-semibold text-red-400">{downCount} check{downCount !== 1 ? 's' : ''} down</span>
                )}
                <span className="text-gray-600"> · {checks.length} total</span>
              </p>
            </div>

            <div className="mb-8 grid gap-3 sm:grid-cols-2">
              {checks.map(check => (
                <CheckCard key={check.check_id} check={check} />
              ))}
            </div>
          </>
        )}

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
