<script lang="ts">
  import { api, setLoginHandler } from './lib/api'
  import { router, link } from './lib/router.svelte'
  import Logo from './lib/ui/Logo.svelte'
  import ThemeToggle from './lib/ui/ThemeToggle.svelte'
  import Login from './pages/Login.svelte'
  import Sites from './pages/Sites.svelte'
  import Site from './pages/Site.svelte'
  import Settings from './pages/Settings.svelte'
  import { applyAccent } from './lib/accent'
  import { pageIn } from './lib/motion'

  let authRequired = $state(false)
  let needsLogin = $state<boolean | null>(null)
  let title = $state('Glance')

  // Accent and title are public so the login screen matches too.
  $effect(() => {
    api
      .theme()
      .then((t) => {
        applyAccent(t.accent)
        title = t.title || 'Glance'
      })
      .catch(() => {})
    // Re-derive the tint family when the theme flips.
    const mq = matchMedia('(prefers-color-scheme: dark)')
    const redo = () => api.theme().then((t) => applyAccent(t.accent)).catch(() => {})
    mq.addEventListener('change', redo)
    const obs = new MutationObserver(redo)
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
    return () => {
      mq.removeEventListener('change', redo)
      obs.disconnect()
    }
  })

  async function boot() {
    try {
      const me = await api.me()
      authRequired = me.auth_required
      needsLogin = me.auth_required && !me.authenticated
    } catch {
      needsLogin = true
    }
  }
  $effect(() => {
    setLoginHandler(() => (needsLogin = true))
    boot()
    return () => setLoginHandler(null)
  })
  async function signOut() {
    await api.logout().catch(() => {})
    needsLogin = true
  }
  const route = $derived(router.route)
</script>

<div class="page" class:wide={route.name === 'site'}>
  <header>
    <a href="/" onclick={link} class="brand"><Logo {title} crumb={route.name === 'site' ? 'Analytics' : route.name === 'settings' ? 'Settings' : ''} /></a>
    <div class="right">
      {#if route.name !== 'sites'}<a href="/" onclick={link}>← Websites</a>{/if}
      {#if needsLogin === false && route.name !== 'settings'}
        <a href="/settings" onclick={link} class="gear" title="Settings" aria-label="Settings">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1.08-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 8.9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 8.9 4.6a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 8.9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" /></svg>
        </a>
      {/if}
      {#if authRequired && needsLogin === false}<button type="button" class="plain" onclick={signOut}>Sign out</button>{/if}
      <ThemeToggle />
    </div>
  </header>

  {#if needsLogin}
    <Login onsuccess={boot} />
  {:else if needsLogin === false}
    {#key router.path}
      <div in:pageIn class="body">
        {#if route.name === 'sites'}
          <Sites />
        {:else if route.name === 'site'}
          <Site id={route.params.id} />
        {:else if route.name === 'settings'}
          <Settings ontitle={(t) => (title = t || 'Glance')} />
        {:else}
          <p class="muted">Nothing here. <a href="/" onclick={link}>Back to websites</a>.</p>
        {/if}
      </div>
    {/key}
  {/if}
</div>

<style>
  .page.wide { --up-page-max: 800px; gap: 36px; }
  header { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
  .brand { color: inherit; }
  .right { display: flex; align-items: center; gap: 18px; font: var(--up-type-ui); }
  .plain { background: none; border: none; padding: 0; cursor: pointer; font: var(--up-type-ui); color: var(--up-text-muted); }
  .plain:hover { color: var(--up-ink); }
  .gear { display: inline-flex; color: var(--up-text-muted); }
  .gear:hover { color: var(--up-ink); }
  .body { display: flex; flex-direction: column; gap: 36px; }
</style>
