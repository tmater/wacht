import StatusBadge from './StatusBadge.jsx'

export default function CheckRow({ check, statusCheck, probesUp, probesTotal, onEdit }) {
  const up = statusCheck?.status === 'up'
  const hasStatus = !!statusCheck
  const badgeStatus = !hasStatus ? 'pending' : up ? 'up' : 'down'

  return (
    <div className="flex items-center justify-between py-2">
      <div className="min-w-0 flex items-center gap-3">
        <div className="min-w-0 flex items-center gap-2">
          <p className="font-mono text-sm text-gray-100">{check.Target}</p>
          {statusCheck?.incident_since && (
            <p className="text-xs text-red-400"> down since {new Date(statusCheck.incident_since).toLocaleString()}</p>
          )}
        </div>
      </div>
      <div className="flex items-center gap-3 shrink-0">
        <button onClick={onEdit} className="text-xs text-gray-500 hover:text-gray-300">Edit</button>
        <span className="text-xs text-gray-600 uppercase">{check.Type}</span>
        {probesTotal > 0 && (
          <span className="text-xs text-gray-500">{probesUp}/{probesTotal}</span>
        )}
        <StatusBadge status={badgeStatus} />

      </div>
    </div>
  )
}
