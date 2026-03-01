import * as ui from './ui.js'

const BASE_NAV_LINKS = [
  { label: 'Dashboard', page: 'dashboard' },
]

export default function Navbar({ email, page, onNavigate, onLogout, appName = 'Wacht', navExtra = null }) {
  const links = [...BASE_NAV_LINKS, ...(navExtra ?? [])]

  return (
    <nav className="border-b border-gray-700 bg-gray-800 px-6 py-3">
      <div className="mx-auto max-w-3xl flex items-center justify-between">

        {/* Logo */}
        <span className="text-sm font-bold text-gray-100">{appName}</span>

        {/* Nav links */}
        <div className="flex items-center gap-6">
          {links.map(link => (
            link.disabled
              ? <span key={link.page} className="text-xs text-gray-600 cursor-not-allowed">{link.label}</span>
              : <button
                  key={link.page}
                  onClick={() => onNavigate(link.page)}
                  className={`text-xs ${page === link.page ? 'text-gray-100 font-semibold' : 'text-gray-400 hover:text-gray-200'}`}
                >
                  {link.label}
                </button>
          ))}
        </div>

        {/* User */}
        <div className="flex items-center gap-4">
          <span className="text-xs text-gray-500">{email}</span>
          <button onClick={() => onNavigate('account')} className={`text-xs ${page === 'account' ? 'text-gray-100 font-semibold' : 'text-gray-400 hover:text-gray-200'}`}>Account</button>
          <button onClick={onLogout} className={ui.btn.ghost}>Sign out</button>
        </div>

      </div>
    </nav>
  )
}
