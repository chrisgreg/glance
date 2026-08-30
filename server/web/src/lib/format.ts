import countries from './countries.json'

/** 12400 -> "12.4k", 980 -> "980", 1500000 -> "1.5m". */
export function fmtNum(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1).replace(/\.0$/, '') + 'm'
  if (n >= 10_000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'k'
  return n.toLocaleString('en')
}

/** Percentage change as "+8%" / "-3%"; "" when there is no baseline. */
/** Minor units (cents, pence) in a currency, compact for large sums. */
export function fmtMoney(minor: number, currency: string): string {
  const code = (currency || 'usd').toUpperCase()
  const amount = minor / 100
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: code,
      maximumFractionDigits: Math.abs(amount) >= 1000 ? 0 : 2,
      minimumFractionDigits: Math.abs(amount) >= 1000 ? 0 : 2,
    }).format(amount)
  } catch {
    return `${amount.toFixed(2)} ${code}`
  }
}

export function fmtDelta(now: number, prev: number): string {
  if (prev === 0) return now > 0 ? 'new' : ''
  const d = Math.round(((now - prev) / prev) * 100)
  if (d === 0) return '0%'
  return (d > 0 ? '+' : '') + d + '%'
}

/** "1.62" views per visitor. */
export function fmtRatio(views: number, visitors: number): string {
  if (visitors === 0) return '0'
  return (views / visitors).toFixed(2).replace(/\.?0+$/, '')
}

const short = new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric' })
const hour = new Intl.DateTimeFormat('en', { hour: 'numeric', minute: '2-digit' })
const dayHour = new Intl.DateTimeFormat('en', { weekday: 'short', hour: 'numeric' })
const longDay = new Intl.DateTimeFormat('en', { weekday: 'short', month: 'short', day: 'numeric' })

/** Label for a series point given the bucket size and range. */
export function fmtPoint(iso: string, bucket: 'hour' | 'day', range: string): string {
  const d = new Date(iso)
  if (bucket === 'day') return longDay.format(d)
  if (range === '24h') return hour.format(d)
  return `${short.format(d)} ${hour.format(d)}`
}

export const fmtDay = (iso: string) => short.format(new Date(iso))
export const fmtDayHour = (iso: string) => dayHour.format(new Date(iso))

export const RANGE_LABEL: Record<string, string> = { '24h': '24h', '7d': '7d', '30d': '30d', '90d': '90d' }
export const RANGE_START: Record<string, string> = { '24h': '24 hours ago', '7d': '7 days ago', '30d': '30 days ago', '90d': '90 days ago' }

type CountryRow = [string, number, number]
const table = countries as unknown as Record<string, CountryRow>

export function countryName(cc: string): string {
  if (!cc) return 'Unknown'
  return table[cc]?.[0] ?? cc
}

export function countryCentroid(cc: string): [number, number] | null {
  const row = table[cc]
  if (!row || cc === 'XX' || !cc) return null
  return [row[2], row[1]] // [lng, lat]
}

/** Regional-indicator flag emoji; falls back to the code where emoji flags do not render. */
export function flag(cc: string): string {
  if (!cc || cc.length !== 2 || cc === 'XX') return ''
  return String.fromCodePoint(...[...cc.toUpperCase()].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65))
}
