// Theme: 'light' | 'dark' | 'system', stored in localStorage and applied as
// data-theme on <html>. index.html applies the stored value before Svelte
// mounts so the page does not flash.

export type Theme = 'light' | 'dark' | 'system'
const KEY = 'up-theme'

function read(): Theme {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'light' || v === 'dark') return v
  } catch {}
  return 'system'
}

const state = $state({ theme: read() })

function apply(t: Theme) {
  const root = document.documentElement
  if (t === 'system') root.removeAttribute('data-theme')
  else root.setAttribute('data-theme', t)
}

export const theme = {
  get value() {
    return state.theme
  },
  /** Effective theme after resolving 'system'. */
  get resolved(): 'light' | 'dark' {
    if (state.theme !== 'system') return state.theme
    return typeof matchMedia !== 'undefined' && matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  },
  set(t: Theme) {
    state.theme = t
    try {
      if (t === 'system') localStorage.removeItem(KEY)
      else localStorage.setItem(KEY, t)
    } catch {}
    apply(t)
  },
  /** Cycle light → dark → system. */
  next() {
    theme.set(state.theme === 'light' ? 'dark' : state.theme === 'dark' ? 'system' : 'light')
  },
}
