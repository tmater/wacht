// Shared UI style tokens — keep in sync with wacht-cloud/web/src/ui.js

export const page = 'min-h-screen bg-gray-900 p-6'

export const card = 'rounded-lg border border-gray-700 bg-gray-800'

export const sectionHeader = 'text-xs font-semibold uppercase tracking-wider text-gray-500'

export const label = 'block text-xs text-gray-400 mb-1'

export const input = 'w-full rounded bg-gray-700 border border-gray-600 px-3 py-2 text-sm text-gray-100 placeholder-gray-500 disabled:opacity-50 focus:outline-none focus:border-gray-400'

export const inputSm = 'w-full rounded bg-gray-700 border border-gray-600 px-3 py-1.5 text-sm text-gray-100 placeholder-gray-500 disabled:opacity-50 focus:outline-none focus:border-gray-400'

export const select = inputSm

export const btn = {
  primary:   'rounded bg-indigo-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-indigo-500 disabled:opacity-50',
  primaryMd: 'rounded bg-indigo-600 px-3 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50',
  secondary: 'rounded bg-gray-700 px-3 py-1.5 text-xs font-semibold text-gray-300 hover:bg-gray-600',
  danger:    'rounded bg-red-900 px-3 py-1.5 text-xs font-semibold text-red-300 hover:bg-red-800',
  ghost:     'text-xs text-gray-500 hover:text-gray-300',
}

export const errorBox = 'rounded-lg border border-red-800 bg-red-950 p-3 text-sm text-red-400'
export const errorText = 'text-xs text-red-400'
export const successText = 'text-xs text-green-400'
