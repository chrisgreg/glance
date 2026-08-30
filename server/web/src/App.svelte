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
          <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="7" cy="7" r="2" /><path d="M7 1.5v1.3M7 11.2v1.3M1.5 7h1.3M11.2 7h1.3M3.1 3.1l.9.9M10 10l.9.9M3.1 10.9l.9-.9M10 4l.9-.9" /></svg>
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
  .page.wide { --up-page-max: 760px; gap: 36px; }
  header { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
  .brand { color: inherit; }
  .right { display: flex; align-items: center; gap: 18px; font: var(--up-type-ui); }
  .plain { background: none; border: none; padding: 0; cursor: pointer; font: var(--up-type-ui); color: var(--up-text-muted); }
  .plain:hover { color: var(--up-ink); }
  .gear { display: inline-flex; color: var(--up-text-faint); }
  .gear:hover { color: var(--up-ink); }
  .body { display: flex; flex-direction: column; gap: 36px; }
</style>
