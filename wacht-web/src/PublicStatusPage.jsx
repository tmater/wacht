import { useEffect, useState } from 'react'
import { API_URL, REFRESH_INTERVAL_MS } from './api.js'
import StatusBadge from './StatusBadge.jsx'
import * as ui from './ui.js'

export default function PublicStatusPage({ slug, appName = 'Wacht' }) {
  const [checks, setChecks] = useState([])
  const [lastUpdated, setLastUpdated] = useState(null)
  const [notFound, setNotFound] = useState(false)
  const [error, setError] = useState(null)

  useEffect(() => {
    let cancelled = false

    async function fetchStatus() {
      try {
        const res = await fetch(`${API_URL}/api/public/status/${encodeURIComponent(slug)}`)
        if (res.status === 404) {
          if (!cancelled) {
            setNotFound(true)
            setError(null)
          }
          return
        }
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const data = await res.json()
        if (cancelled) return
        setChecks(data.checks ?? [])
        setLastUpdated(new Date())
        setNotFound(false)
        setError(null)
      } catch (e) {
        if (!cancelled) setError(e.message)
      }
    }

    fetchStatus()
    const id = setInterval(fetchStatus, REFRESH_INTERVAL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [slug])

  const allOperational = checks.length > 0 && checks.every(check => check.status === 'up')
  const downCount = checks.filter(check => check.status === 'down').length
  const errorCount = checks.filter(check => check.status === 'error').length
  const pendingCount = checks.filter(check => check.status === 'pending').length

  return (
    <div className={ui.page}>
      <div className="mx-auto max-w-3xl">
        <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-gray-500">{appName}</p>
        <h1 className="mb-3 text-2xl font-semibold text-gray-100">Service status</h1>

        {notFound && (
          <div className={`${ui.card} p-6`}>
            <p className="text-sm text-gray-300">Status page not found.</p>
          </div>
        )}

        {!notFound && (
          <>
            <div className={`${ui.card} mb-4 p-4`}>
              <p className="text-sm text-gray-300">
                {checks.length === 0 && 'No checks published yet.'}
                {allOperational && 'All checks operational.'}
                {downCount > 0 && `${downCount} check${downCount !== 1 ? 's' : ''} down.`}
                {downCount === 0 && errorCount > 0 && `Monitoring degraded for ${errorCount} check${errorCount !== 1 ? 's' : ''}.`}
                {downCount === 0 && errorCount === 0 && !allOperational && pendingCount > 0 && `${pendingCount} check${pendingCount !== 1 ? 's are' : ' is'} still pending.`}
              </p>
              {lastUpdated && <p className="mt-2 text-xs text-gray-500">Updated {lastUpdated.toLocaleTimeString()}</p>}
            </div>

            {error && <div className={`mb-4 ${ui.errorBox}`}>Could not reach server: {error}</div>}

            <div className={`${ui.card} divide-y divide-gray-700 px-4`}>
              {checks.map(check => (
                <div key={check.check_id} className="flex items-center gap-4 py-3">
                  <p className="min-w-0 flex-1 truncate font-mono text-sm text-gray-100">{check.check_id}</p>
                  {check.incident_since && (
                    <p className="shrink-0 text-xs text-red-400">
                      {check.status === 'down' ? 'down since ' : 'incident since '}
                      {new Date(check.incident_since).toLocaleString()}
                    </p>
                  )}
                  <StatusBadge status={check.status} />
                </div>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  )
}
