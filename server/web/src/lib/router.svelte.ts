// Minimal history-based router. The Go server serves index.html for any
// unknown path, so deep links work.

export interface Route {
  name: 'sites' | 'site' | 'settings' | 'notfound'
  params: Record<string, string>
}

function parse(path: string): Route {
  const p = path.replace(/\/+$/, '') || '/'
  if (p === '/') return { name: 'sites', params: {} }
  if (p === '/settings') return { name: 'settings', params: {} }
  const m = p.match(/^\/s\/([^/]+)$/)
  if (m) return { name: 'site', params: { id: decodeURIComponent(m[1]) } }
  return { name: 'notfound', params: {} }
}

const state = $state({ path: typeof location !== 'undefined' ? location.pathname : '/' })

export const router = {
  get path() {
    return state.path
  },
  get route(): Route {
    return parse(state.path)
  },
  navigate(path: string, replace = false) {
    if (path === state.path) return
    if (replace) history.replaceState(null, '', path)
    else history.pushState(null, '', path)
    state.path = path
    window.scrollTo(0, 0)
  },
}

if (typeof window !== 'undefined') {
  window.addEventListener('popstate', () => {
    state.path = location.pathname
  })
}

/** Click handler for <a> elements: intercept plain left-clicks. */
export function link(e: MouseEvent) {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
  const a = (e.currentTarget as HTMLAnchorElement) ?? null
  const href = a?.getAttribute('href')
  if (!href || !href.startsWith('/')) return
  e.preventDefault()
  router.navigate(href)
}

export { parse as parseRoute }
