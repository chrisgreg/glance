<script lang="ts">
  // Centered hairline card over a dimmed backdrop. Closes on Escape, on the
  // backdrop, or via the × button.
  import type { Snippet } from 'svelte'
  import { fade, scale } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { dur } from '../motion'

  let { title, subtitle = '', onclose, children }: { title: string; subtitle?: string; onclose: () => void; children: Snippet } = $props()

  $effect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onclose()
    window.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  })
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="backdrop" transition:fade={{ duration: dur(140) }} onclick={onclose} onkeydown={(e) => e.key === 'Escape' && onclose()}>
  <div
    class="card"
    role="dialog"
    aria-modal="true"
    aria-label={title}
    tabindex="-1"
    transition:scale={{ start: 0.97, duration: dur(180), easing: cubicOut }}
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
  >
    <div class="head">
      <div>
        <div class="title">{title}</div>
        {#if subtitle}<div class="subtitle">{subtitle}</div>{/if}
      </div>
      <button type="button" class="close" aria-label="Close" onclick={onclose}>×</button>
    </div>
    <div class="body">{@render children()}</div>
  </div>
</div>

<style>
  .backdrop { position: fixed; inset: 0; z-index: 200; background: rgba(23, 24, 31, 0.4); display: flex; align-items: center; justify-content: center; padding: 24px; }
  .card { width: 100%; max-width: 560px; max-height: min(80vh, 720px); background: var(--up-bg); border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); display: flex; flex-direction: column; overflow: hidden; }
  .head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 20px 20px 12px; }
  .title { font: var(--up-type-setting); font-weight: 700; }
  .subtitle { font: var(--up-type-meta); color: var(--up-text-muted); margin-top: 2px; }
  .close { background: none; border: none; cursor: pointer; font-size: 18px; line-height: 1; color: var(--up-text-faint); padding: 2px; }
  .close:hover { color: var(--up-ink); }
  .body { overflow: auto; padding: 0 20px 20px; }
</style>
