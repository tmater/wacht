import StatusBadge from './StatusBadge.jsx'

function formatDuration(ms) {
  if (ms == null) return null
  const totalSeconds = Math.floor(ms / 1000)
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (hours > 0) return `${hours}h ${minutes}m`
  if (minutes > 0) return `${minutes}m ${seconds}s`
  return `${seconds}s`
}

export default function IncidentRow({ incident }) {
  const started = new Date(incident.started_at)
  const duration = incident.duration_ms != null
    ? formatDuration(incident.duration_ms)
    : formatDuration(Date.now() - started.getTime())
  const open = incident.resolved_at == null

  return (
    <div className="flex items-center justify-between gap-4 py-2 text-sm">
      <span className="font-mono text-gray-300 truncate">{incident.check_id}</span>
      <span className="text-gray-500 shrink-0">{started.toLocaleString()}</span>
      <span className="text-gray-500 shrink-0">{duration}</span>
      <StatusBadge status={open ? 'open' : 'resolved'} />
    </div>
  )
}
