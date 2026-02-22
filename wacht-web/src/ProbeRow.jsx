export default function ProbeRow({ probe }) {
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
