function configuredBasePath() {
  return import.meta.env.BASE_URL ?? '/'
}

export function normalizedBasePath() {
  const basePath = configuredBasePath()
  if (basePath === '/') return '/'
  return basePath.endsWith('/') ? basePath : `${basePath}/`
}

export function appURL(pathname) {
  const relativePath = pathname.startsWith('/') ? pathname.slice(1) : pathname
  return new URL(relativePath, new URL(normalizedBasePath(), window.location.origin)).toString()
}

export function stripBasePath(pathname) {
  const basePath = normalizedBasePath().replace(/\/$/, '')
  if (!basePath || basePath === '/') return pathname
  return pathname.startsWith(basePath) ? pathname.slice(basePath.length) || '/' : pathname
}
