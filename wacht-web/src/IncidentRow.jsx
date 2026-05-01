import { useEffect, useState } from 'react'
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

const deliveryStyles = {
  delivered: 'bg-green-900 text-green-300',
  none: 'bg-gray-950 text-gray-500',
  skipped: 'bg-gray-700 text-gray-300',
  retrying: 'bg-amber-900 text-amber-300',
  sending: 'bg-blue-900 text-blue-300',
  pending: 'bg-gray-800 text-gray-400',
}

function notificationState(notification) {
  if (!notification) return null
  switch (notification.state) {
    case 'delivered':
      return 'delivered'
    case 'retrying':
      return 'retrying'
    case 'processing':
      return 'sending'
    case 'superseded':
      return 'skipped'
    case 'pending':
      return 'pending'
    default:
      return 'pending'
  }
}

function incidentDeliveryStatus(incident) {
  const states = [
    notificationState(incident.down_notification),
    notificationState(incident.up_notification),
  ].filter(Boolean)

  if (states.length === 0) {
    return 'none'
  }
  if (states.includes('retrying')) {
    return 'retrying'
  }
  if (states.includes('sending')) {
    return 'sending'
  }
  if (states.includes('pending')) {
    return 'pending'
  }
  if (states.includes('delivered')) {
    return 'delivered'
  }
  if (states.includes('skipped')) {
    return 'skipped'
  }
  return 'none'
}

function DeliveryBadge({ status }) {
  return (
    <span className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${deliveryStyles[status] ?? deliveryStyles.pending}`}>
      {status.toUpperCase()}
    </span>
  )
}

function trimDuration(duration) {
  if (!duration) return duration
  return duration.replace(' 0s', '')
}

export default function IncidentRow({ incident }) {
  const started = new Date(incident.started_at)
  const open = incident.resolved_at == null
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!open || incident.duration_ms != null) {
      return undefined
    }
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [incident.duration_ms, open])

  const duration = incident.duration_ms != null
    ? trimDuration(formatDuration(incident.duration_ms))
    : trimDuration(formatDuration(now - started.getTime()))
  const deliveryStatus = incidentDeliveryStatus(incident)

  return (
    <div className="flex items-center gap-4 py-2">
      <p className="font-mono text-sm text-gray-300 w-30 shrink-0 truncate">{incident.check_name}</p>
      <div className="flex-1 min-w-0">
        <p className="text-xs text-gray-500 truncate">{started.toLocaleString()}</p>
      </div>
      <div className="flex items-center gap-3 shrink-0">
        <span className="text-xs text-gray-500 w-14 text-right">{duration}</span>
        <DeliveryBadge status={deliveryStatus} />
        <StatusBadge status={open ? 'open' : 'resolved'} />
      </div>
    </div>
  )
}
