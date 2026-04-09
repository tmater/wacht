import StatusBadge from './StatusBadge.jsx'

export default function ProbeRow({ probe }) {
  const hasSeenHeartbeat = Boolean(probe.last_seen_at)
  const status = probe.status === 'online'
    ? 'up'
    : probe.status === 'error'
      ? 'error'
      : hasSeenHeartbeat
        ? 'down'
        : 'pending'
  const detail = probe.status === 'error' && probe.last_error
    ? probe.last_error
    : hasSeenHeartbeat
      ? new Date(probe.last_seen_at).toLocaleTimeString()
      : 'Never seen'

  return (
    <div className="flex items-center justify-between py-2">
      <p className="font-mono text-sm text-gray-300">{probe.probe_id}</p>
      <div className="flex items-center gap-3">
        <p className="text-xs text-gray-500">{detail}</p>
        <StatusBadge status={status} />
      </div>
    </div>
  )
}
