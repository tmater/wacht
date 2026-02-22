export const API_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:8080'
export const REFRESH_INTERVAL_MS = 30_000
export const CHECK_TYPES = ['http', 'tcp', 'dns']

export function getToken() { return localStorage.getItem('wacht_token') }
export function setToken(t) { localStorage.setItem('wacht_token', t) }
export function clearToken() { localStorage.removeItem('wacht_token') }
export function getEmail() { return localStorage.getItem('wacht_email') }
export function saveEmail(e) { localStorage.setItem('wacht_email', e) }
export function clearEmail() { localStorage.removeItem('wacht_email') }

export function authHeaders() {
  return { 'Authorization': `Bearer ${getToken()}`, 'Content-Type': 'application/json' }
}
