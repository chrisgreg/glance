<script lang="ts" generics="T extends string | number">
  // The signature underline control: 13px/600 text, inactive gray, active ink
  // with a 1.5px periwinkle underline that slides between options.
  let {
    options,
    value,
    onchange,
    gap = 18,
  }: { options: { value: T; label: string }[]; value: T; onchange: (v: T) => void; gap?: number } = $props()

  let nav = $state<HTMLElement | null>(null)
  let els = $state<Record<string, HTMLElement>>({})
  let bar = $state({ left: 0, width: 0, ready: false })

  $effect(() => {
    const el = els[String(value)]
    if (!el || !nav) return
    const measure = () => (bar = { left: el.offsetLeft, width: el.offsetWidth, ready: true })
    measure()
    document.fonts?.ready.then(measure)
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  })
</script>

<div class="seg" style="gap: {gap}px" bind:this={nav}>
  {#each options as o (o.value)}
    <button type="button" class:active={o.value === value} bind:this={els[String(o.value)]} onclick={() => onchange(o.value)}>{o.label}</button>
  {/each}
  <span class="bar" class:ready={bar.ready} style="left: {bar.left}px; width: {bar.width}px"></span>
</div>

<style>
  .seg { display: flex; align-items: center; position: relative; }
  button {
    font: var(--up-type-ui);
    background: none;
    border: none;
    padding: 0 0 3px;
    cursor: pointer;
    color: var(--up-text-inactive);
    white-space: nowrap;
  }
  button:hover { color: var(--up-ink); }
  button.active { color: var(--up-ink); }
  .bar {
    position: absolute;
    bottom: 0;
    height: 1.5px;
    background: var(--up-accent);
    pointer-events: none;
    opacity: 0;
    transition: left 200ms cubic-bezier(0.2, 0, 0, 1), width 200ms cubic-bezier(0.2, 0, 0, 1), opacity 120ms ease-out;
  }
  .bar.ready { opacity: 1; }
  @media (prefers-reduced-motion: reduce) {
    .bar { transition: none; }
  }
</style>
