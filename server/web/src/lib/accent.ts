// Accent colour: derive the accent token family from one hex and apply it to
// the document, so the whole UI (including the map and chart) follows.

function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace('#', '')
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)]
}
function rgbToHex([r, g, b]: [number, number, number]): string {
  return '#' + [r, g, b].map((v) => Math.round(Math.max(0, Math.min(255, v))).toString(16).padStart(2, '0')).join('')
}
/** Mix a colour toward another by t (0..1). */
function mix(a: [number, number, number], b: [number, number, number], t: number): [number, number, number] {
  return [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t, a[2] + (b[2] - a[2]) * t]
}

function luminance([r, g, b]: [number, number, number]): number {
  const f = (v: number) => {
    const c = v / 255
    return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4)
  }
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b)
}
function contrast(a: [number, number, number], b: [number, number, number]): number {
  const la = luminance(a)
  const lb = luminance(b)
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
}
/** Darken (light theme) or lighten (dark theme) until text on the page background reads at 3:1. */
function readable(c: [number, number, number], bg: [number, number, number], towards: [number, number, number]): [number, number, number] {
  let out = c
  for (let i = 0; i < 12 && contrast(out, bg) < 3; i++) out = mix(out, towards, 0.12)
  return out
}

export const DEFAULT_ACCENT = '#7C83E8'
export const SWATCHES = [
  { name: 'Periwinkle', hex: '#7C83E8' },
  { name: 'Mint', hex: '#5FBF9F' },
  { name: 'Blush', hex: '#E88CB0' },
  { name: 'Amber', hex: '#E8B34C' },
]

export function isHex(v: string): boolean {
  return /^#[0-9a-fA-F]{6}$/.test(v)
}

/** Apply an accent hex to :root. Invalid input restores the default. */
export function applyAccent(hex: string) {
  if (!isHex(hex)) hex = DEFAULT_ACCENT
  const root = document.documentElement.style
  const c = hexToRgb(hex)
  const white: [number, number, number] = [253, 253, 254]
  const ink: [number, number, number] = [26, 27, 37]
  const darkBg: [number, number, number] = [19, 20, 25]
  const dark = document.documentElement.dataset.theme === 'dark' || (!document.documentElement.dataset.theme && matchMedia('(prefers-color-scheme: dark)').matches)
  const bg = dark ? darkBg : white
  // Text and controls get a contrast-checked shade; lines, dots and arcs keep the raw hue.
  const text = readable(c, bg, dark ? white : ink)
  root.setProperty('--up-accent', rgbToHex(text))
  root.setProperty('--up-accent-hover', rgbToHex(mix(text, dark ? white : ink, 0.2)))
  root.setProperty('--up-accent-line', hex)
  root.setProperty('--up-accent-tint', rgbToHex(dark ? mix(c, darkBg, 0.72) : mix(c, white, 0.78)))
  root.setProperty('--up-operational', rgbToHex(dark ? mix(c, darkBg, 0.55) : mix(c, white, 0.6)))
  root.setProperty('--up-operational-strong', rgbToHex(dark ? mix(c, white, 0.15) : readable(mix(c, white, 0.2), bg, ink)))
}

/** Remove overrides so the stylesheet's defaults apply again. */
export function clearAccent() {
  const root = document.documentElement.style
  for (const k of ['--up-accent', '--up-accent-hover', '--up-accent-line', '--up-accent-tint', '--up-operational', '--up-operational-strong']) root.removeProperty(k)
}
