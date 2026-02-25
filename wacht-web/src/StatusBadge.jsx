const styles = {
  up:      'bg-green-900 text-green-300',
  down:    'bg-red-900 text-red-300',
  pending: 'bg-gray-700 text-gray-400',
}

export default function StatusBadge({ status }) {
  return (
    <span className={`w-20 text-center rounded-full px-2.5 py-0.5 text-xs font-semibold ${styles[status] ?? styles.pending}`}>
      {status.toUpperCase()}
    </span>
  )
}