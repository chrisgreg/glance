// Thin typed client for the Glance API.

export type Range = '24h' | '7d' | '30d' | '90d'
export const RANGES: Range[] = ['24h', '7d', '30d', '90d']

export interface Point {
  t: string
  pageviews: number
  visitors: number
}

export interface Row {
  key: string
  pageviews: number
  visitors: number
}

export interface Totals {
  pageviews: number
  visitors: number
}

export type Dim = 'page' | 'ref' | 'country' | 'region' | 'device' | 'browser' | 'os' | 'event' | 'utm_source' | 'utm_campaign'

export interface Marker {
  t: string
  ref: string
  visitors: number
}

/** Dimension to key. An empty value is a real filter (direct, unknown). */
export type Filters = Partial<Record<Dim, string>>

export function filterQuery(filters: Filters): string {
  const q = new URLSearchParams()
  for (const [dim, key] of Object.entries(filters)) if (key !== undefined) q.set(dim, key)
  const s = q.toString()
  return s ? '&' + s : ''
}

export interface Summary {
  range: Range
  from: string
  to: string
  bucket: 'hour' | 'day'
  totals: Totals
  previous: Totals
  series: Point[]
  markers: Marker[]
  breakdowns: Record<Dim, Row[]>
  filters?: Filters
  truncated?: boolean
  retention_days?: number
}

export interface Live {
  total: number
  countries: Row[]
  recent: { at: string; country: string; path: string }[]
  minutes: number[] // distinct visitors per minute, last 30, oldest first
  total_30m: number
}

export interface SiteCard {
  visitors: number
  previous: number
  pageviews: number
  spark: Point[]
}

export interface Site {
  id: string
  name: string
  domain: string
  home_country: string
  has_favicon: boolean
  position: number
  created_at: string
  updated_at: string
  card: SiteCard
  live: number
}

export interface AuthState {
  auth_required: boolean
  authenticated: boolean
}

export interface Status {
  version: string
  sites: number
  raw_events: number
  daily_rows: number
  db_bytes: number
  uptime_seconds: number
  written: number
  dropped: number
  admin_auth: boolean
  env_token_set: boolean
}

export interface GeneralSettings {
  accent: string
  title: string
  mcp_enabled: boolean
  retention_days: number
  retention_from_env: boolean
}

export interface Token {
  id: string
  name: string
  prefix: string
  created_at: string
  last_used_at?: string
}

export interface GoogleConnection {
  site_id: string
  property: string
  email: string
  connected_at: string
  synced_at: string
  sync_error: string
}

export interface GoogleStatus {
  configured: boolean
  connected: boolean
  connection?: GoogleConnection
  available_properties?: string[]
  needs_reconnect: boolean
  latest_day?: string
}

export interface SearchTerm {
  query: string
  clicks: number
  impressions: number
  position: number
}

export interface PolarConnection {
  site_id: string
  server: string
  product_ids: string
  has_webhook_secret: boolean
  connected_at: string
  synced_at: string
  sync_error: string
}

export interface PolarStatus {
  connected: boolean
  connection?: PolarConnection
  webhook_url: string
  orders: number
}

export interface RevenuePoint {
  t: string
  revenue: number // minor units
  orders: number
}

export interface RevenueTotals {
  revenue: number
  orders: number
}

export interface RevenueRow {
  key: string
  revenue: number
  orders: number
}

export type RevenueDim = 'ref' | 'source' | 'campaign' | 'landing' | 'country' | 'product'

export interface Revenue {
  range: Range
  currency: string
  totals: RevenueTotals
  previous: RevenueTotals
  series: RevenuePoint[]
  breakdowns: Record<RevenueDim, RevenueRow[]>
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

let onLoginRequired: (() => void) | null = null
export function setLoginHandler(fn: (() => void) | null) {
  onLoginRequired = fn
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (res.status === 204) return undefined as T
  const text = await res.text()
  let parsed: any = null
  try {
    parsed = text ? JSON.parse(text) : null
  } catch {
    parsed = null
  }
  if (!res.ok) {
    if (res.status === 401 && parsed?.error === 'login_required') onLoginRequired?.()
    throw new ApiError(res.status, parsed?.error ?? 'error', parsed?.message ?? `Request failed (${res.status})`)
  }
  return parsed as T
}

export const api = {
  me: () => request<AuthState>('GET', '/api/v1/auth/me'),
  login: (username: string, password: string) => request<AuthState>('POST', '/api/v1/auth/login', { username, password }),
  logout: () => request<void>('POST', '/api/v1/auth/logout'),

  sites: () => request<{ sites: Site[] }>('GET', '/api/v1/sites'),
  site: (id: string) => request<Site>('GET', `/api/v1/sites/${id}`),
  createSite: (input: { name?: string; domain: string }) => request<Site>('POST', '/api/v1/sites', input),
  updateSite: (id: string, patch: Partial<Pick<Site, 'name' | 'domain' | 'home_country'>>) => request<Site>('PATCH', `/api/v1/sites/${id}`, patch),
  deleteSite: (id: string) => request<void>('DELETE', `/api/v1/sites/${id}`),
  reorderSites: (ids: string[]) => request<{ sites: Site[] }>('POST', '/api/v1/sites/reorder', { ids }),
  refreshFavicon: (id: string) => request<Site>('POST', `/api/v1/sites/${id}/refresh-favicon`),
  live: (id: string) => request<Live>('GET', `/api/v1/sites/${id}/live`),
  breakdown: (id: string, dim: Dim, range: Range, filters: Filters = {}) => request<{ dim: Dim; range: Range; rows: Row[] }>('GET', `/api/v1/sites/${id}/breakdown?dim=${dim}&range=${range}&limit=500${filterQuery(filters)}`),
  stats: (id: string, range: Range, filters: Filters = {}) => request<{ site: Site; live: number; stats: Summary }>('GET', `/api/v1/sites/${id}/stats?range=${range}${filterQuery(filters)}`),
  status: () => request<Status>('GET', '/api/v1/status'),
  theme: () => request<{ accent: string; title: string }>('GET', '/api/v1/theme'),
  settings: () => request<GeneralSettings>('GET', '/api/v1/settings'),
  updateSettings: (patch: Partial<Omit<GeneralSettings, 'retention_from_env'>>) => request<GeneralSettings>('PATCH', '/api/v1/settings', patch),
  tokens: () => request<{ tokens: Token[]; env_token_set: boolean }>('GET', '/api/v1/tokens'),
  createToken: (name: string) => request<{ token: Token; secret: string }>('POST', '/api/v1/tokens', { name }),
  deleteToken: (id: string) => request<void>('DELETE', `/api/v1/tokens/${id}`),
  rollup: () => request<void>('POST', '/api/v1/rollup'),

  google: (id: string) => request<{ status: GoogleStatus; redirect_uri: string }>('GET', `/api/v1/sites/${id}/google`),
  googleSetProperty: (id: string, property: string) => request<{ status: GoogleStatus; redirect_uri: string }>('PATCH', `/api/v1/sites/${id}/google`, { property }),
  googleDisconnect: (id: string) => request<void>('DELETE', `/api/v1/sites/${id}/google`),
  googleSync: (id: string) => request<{ status: GoogleStatus; redirect_uri: string }>('POST', `/api/v1/sites/${id}/google/sync`),
  searchTerms: (id: string, range: Range) => request<{ range: Range; rows: SearchTerm[] }>('GET', `/api/v1/sites/${id}/search-terms?range=${range}&limit=500`),
}

export const polarApi = {
  status: (id: string) => request<PolarStatus>('GET', `/api/v1/sites/${id}/polar`),
  connect: (id: string, input: { access_token?: string; server?: string; product_ids?: string; webhook_secret?: string }) => request<PolarStatus>('PUT', `/api/v1/sites/${id}/polar`, input),
  disconnect: (id: string) => request<void>('DELETE', `/api/v1/sites/${id}/polar`),
  sync: (id: string) => request<PolarStatus>('POST', `/api/v1/sites/${id}/polar/sync`),
  revenue: (id: string, range: Range, limit = 10) => request<Revenue>('GET', `/api/v1/sites/${id}/revenue?range=${range}&limit=${limit}`),
}

/** Browser destination that starts the Google Search Console connect flow. */
export const googleConnectURL = (id: string) => `/api/v1/sites/${id}/google/connect`

/** URL of a site's stored icon (404 when none). */
export const siteIconURL = (id: string) => `/api/v1/sites/${id}/favicon`
/** URL of a cached referrer icon (404 when none). */
export const refIconURL = (host: string) => `/api/v1/favicon?host=${encodeURIComponent(host)}`
