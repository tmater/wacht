import StatusBadge from './StatusBadge.jsx'
import * as ui from './ui.js'

export default function CheckRow({ check, statusCheck, probesUp, probesTotal, onEdit }) {
  const badgeStatus = statusCheck?.status ?? 'pending'
  const incidentLabel = badgeStatus === 'down' ? 'down since' : 'incident since'

  return (
    <div className="flex items-center gap-4 py-2">
      <p className="font-mono text-sm text-gray-100 w-30 shrink-0 truncate">{check.name}</p>
      <div className="flex-1 min-w-0 flex items-center gap-2">
        <p className="font-mono text-xs text-gray-500 truncate">{check.target}</p>
        {statusCheck?.incident_since && (
          <p className="text-xs text-red-400 shrink-0">{incidentLabel} {new Date(statusCheck.incident_since).toLocaleString()}</p>
        )}
      </div>
      <div className="flex items-center gap-4 shrink-0">
        <span className="text-xs text-gray-600 uppercase w-8 text-center">{check.type}</span>
        <span className="text-xs text-gray-500 w-8 text-center">{probesTotal > 0 ? `${probesUp}/${probesTotal}` : ''}</span>
        <button onClick={onEdit} className={ui.btn.ghost}>Edit</button>
        <StatusBadge status={badgeStatus} />
      </div>
    </div>
  )
}
