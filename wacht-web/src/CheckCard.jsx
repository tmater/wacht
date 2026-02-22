export default function CheckCard({ check, statusCheck, onEdit, onDelete }) {
  const up = statusCheck?.status === 'up'
  const hasStatus = !!statusCheck

  return (
    <div className="rounded-lg border border-gray-700 bg-gray-800 p-4">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="font-mono text-sm font-semibold text-gray-100">{check.ID}</p>
          <p className="mt-0.5 text-xs text-gray-400 break-all">{check.Target}</p>
          <p className="mt-0.5 text-xs text-gray-600 uppercase">{check.Type}</p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {hasStatus && (
            <span className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${up ? 'bg-green-900 text-green-300' : 'bg-red-900 text-red-300'}`}>
              {up ? 'UP' : 'DOWN'}
            </span>
          )}
          <button onClick={onEdit} className="text-xs text-gray-500 hover:text-gray-300">Edit</button>
          <button onClick={onDelete} className="text-xs text-gray-500 hover:text-red-400">Delete</button>
        </div>
      </div>
      {statusCheck?.incident_since && (
        <p className="mt-2 text-xs text-red-400">
          Down since {new Date(statusCheck.incident_since).toLocaleString()}
        </p>
      )}
    </div>
  )
}
