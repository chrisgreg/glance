<script lang="ts">
  let { title = 'UP', crumb = '' }: { title?: string; crumb?: string } = $props()
</script>

<div class="logo">
  <span class="mark"></span>
  <span class="wordmark">{title}</span>
  {#if crumb}<span class="crumb">/ {crumb}</span>{/if}
</div>

<style>
  .logo { display: flex; align-items: center; gap: 10px; color: var(--up-ink); }
  .mark { width: 28px; height: 28px; border-radius: 50%; background: var(--up-accent-line, var(--up-accent)); position: relative; flex-shrink: 0; }
  .mark::after { content: ''; position: absolute; inset: 9px; border-radius: 50%; background: var(--up-bg); }
  .wordmark { font: var(--up-type-wordmark); letter-spacing: 0.02em; }
  .crumb { font: var(--up-type-status-line); color: var(--up-text-faint); }
</style>
