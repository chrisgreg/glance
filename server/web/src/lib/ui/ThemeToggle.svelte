<script lang="ts">
  import { theme } from '../theme.svelte'
  const label = $derived(theme.value === 'system' ? 'Theme: system' : theme.value === 'dark' ? 'Theme: dark' : 'Theme: light')
</script>

<button type="button" class="toggle" title={label} aria-label={label} onclick={() => theme.next()}>
  {#if theme.value === 'dark'}
    <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M11.5 8.5A5 5 0 0 1 5.5 2.5a5 5 0 1 0 6 6z" /></svg>
  {:else if theme.value === 'light'}
    <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><circle cx="7" cy="7" r="2.6" /><path d="M7 1.5v1.4M7 11.1v1.4M1.5 7h1.4M11.1 7h1.4M3.1 3.1l1 1M9.9 9.9l1 1M3.1 10.9l1-1M9.9 4.1l1-1" /></svg>
  {:else}
    <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><circle cx="7" cy="7" r="5.2" /><path d="M7 1.8v10.4" /><path d="M7 1.8a5.2 5.2 0 0 1 0 10.4z" fill="currentColor" stroke="none" /></svg>
  {/if}
</button>

<style>
  .toggle { display: inline-flex; background: none; border: none; padding: 2px; cursor: pointer; color: var(--up-text-faint); }
  .toggle:hover { color: var(--up-ink); }
</style>
