import StatusBadge from './StatusBadge.jsx'

export default function ProbeRow({ probe }) {
  const hasSeenHeartbeat = Boolean(probe.last_seen_at)
  const status = probe.online ? 'up' : (hasSeenHeartbeat ? 'down' : 'pending')

  return (
    <div className="flex items-center justify-between py-2">
      <p className="font-mono text-sm text-gray-300">{probe.probe_id}</p>
      <div className="flex items-center gap-3">
        <p className="text-xs text-gray-500">
          {hasSeenHeartbeat ? new Date(probe.last_seen_at).toLocaleTimeString() : 'Never seen'}
        </p>
        <StatusBadge status={status} />
      </div>
    </div>
  )
}
