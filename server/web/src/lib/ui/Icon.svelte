<script lang="ts">
  // 18–22px circular icon with an inset hairline ring: a site or referrer
  // favicon served by Glance itself, or the "direct" arrow glyph. Hidden when
  // the icon fails to load, leaving a blank circle so rows stay aligned.
  let { src = '', direct = false, size = 18, placeholder = true }: { src?: string; direct?: boolean; size?: number; placeholder?: boolean } = $props()
  let failed = $state(false)
  $effect(() => {
    src
    failed = false
  })
</script>

{#if direct}
  <span class="ring arrow" style="width: {size}px; height: {size}px">
    <svg width="9" height="9" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M1 5 H9 M5.5 1.5 L9 5 L5.5 8.5" /></svg>
  </span>
{:else if src && !failed}
  <img class="ring" {src} alt="" width={size} height={size} loading="lazy" onerror={() => (failed = true)} />
{:else if placeholder}
  <span class="ring blank" style="width: {size}px; height: {size}px"></span>
{/if}

<style>
  .ring { border-radius: 50%; background: var(--up-bg); box-shadow: var(--up-ring-inset); flex-shrink: 0; display: inline-flex; align-items: center; justify-content: center; }
  img { object-fit: cover; }
  .arrow { color: var(--up-text-muted); }
  .blank { background: var(--up-accent-tint); }
</style>
