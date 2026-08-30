<script lang="ts">
  // Headline stat. Deltas: periwinkle when up, soft orange when negative.
  let { label, value, delta = '' }: { label: string; value: string; delta?: string } = $props()
  const negative = $derived(delta.startsWith('-'))
</script>

<div class="stat">
  <div class="label">{label}</div>
  <div class="row">
    <div class="value">{value}</div>
    {#if delta}<div class="delta" class:negative>{delta}</div>{/if}
  </div>
</div>

<style>
  .stat { display: flex; flex-direction: column; gap: 3px; }
  .label { font: var(--up-type-meta); color: var(--up-text-muted); white-space: nowrap; }
  .row { display: flex; align-items: baseline; gap: 8px; }
  .value { font: var(--up-type-metric); letter-spacing: -0.01em; }
  .delta { font: var(--up-type-small); color: var(--up-accent); }
  .delta.negative { color: var(--up-degraded-strong); }
</style>
