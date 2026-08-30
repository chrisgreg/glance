<script lang="ts">
  import { api } from '../lib/api'
  import Input from '../lib/ui/Input.svelte'
  import Button from '../lib/ui/Button.svelte'

  let { onsuccess }: { onsuccess: () => void } = $props()
  let username = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)

  async function submit() {
    busy = true
    error = ''
    try {
      await api.login(username, password)
      onsuccess()
    } catch (e: any) {
      error = e.status === 401 ? 'Wrong username or password.' : e.message
      password = ''
    } finally {
      busy = false
    }
  }
</script>

<div class="login">
  <h2>Sign in</h2>
  <p class="secondary">Use the username and password from the server's environment.</p>
  <form
    onsubmit={(e) => {
      e.preventDefault()
      submit()
    }}
  >
    <Input bind:value={username} placeholder="Username" aria-label="Username" autocomplete="username" required />
    <Input bind:value={password} placeholder="Password" aria-label="Password" type="password" autocomplete="current-password" required />
    {#if error}<p class="error">{error}</p>{/if}
    <Button type="submit" disabled={busy || !username || !password}>{busy ? 'Signing in' : 'Sign in'}</Button>
  </form>
</div>

<style>
  .login { max-width: 320px; display: flex; flex-direction: column; gap: 12px; }
  h2 { font: var(--up-type-setting); }
  p { font: var(--up-type-meta); line-height: 1.5; }
  form { display: flex; flex-direction: column; gap: var(--up-space-3); margin-top: 8px; }
  .error { color: var(--up-degraded-strong); }
</style>
