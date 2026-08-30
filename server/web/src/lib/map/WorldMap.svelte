<script lang="ts">
  // Where visitors come from: arcs from each country's centroid to the
  // site's home country, dots sized by visitors, dark tooltip on hover.
  import Map from './Map.svelte'
  import Land from './Land.svelte'
  import Arcs, { type Arc } from './Arcs.svelte'
  import Marker from './Marker.svelte'
  import type { Row } from '../api'
  import { countryCentroid, countryName, flag, fmtNum } from '../format'

  let { rows, home }: { rows: Row[]; home: string } = $props()

  const points = $derived(
    rows
      .map((r) => ({ cc: r.key, visitors: r.visitors, at: countryCentroid(r.key) }))
      .filter((p): p is { cc: string; visitors: number; at: [number, number] } => p.at !== null),
  )
  const max = $derived(Math.max(1, ...points.map((p) => p.visitors)))
  const hub = $derived(countryCentroid(home) ?? points[0]?.at ?? null)
  const arcs = $derived<Arc[]>(
    hub ? points.filter((p) => p.cc !== home).map((p) => ({ id: p.cc, from: p.at, to: hub!, weight: p.visitors / max })) : [],
  )
  let tip = $state<{ cc: string; visitors: number; x: number; y: number } | null>(null)
  function enter(p: { cc: string; visitors: number }, el: HTMLElement) {
    const r = el.getBoundingClientRect()
    tip = { cc: p.cc, visitors: p.visitors, x: r.left + r.width / 2, y: r.top }
  }
</script>

<div class="wrap">
  <Map center={[10, 22]} zoom={0.9} options={{ scrollZoom: false, dragRotate: false, pitchWithRotate: false, minZoom: 0.6, maxZoom: 4, interactive: true, bounds: [[-168, -56], [180, 78]], fitBoundsOptions: { padding: 8 } }}>
    <Land />
    <Arcs {arcs} />
    {#each points as p (p.cc)}
      <Marker lng={p.at[0]} lat={p.at[1]} onenter={(el) => enter(p, el)} onleave={() => (tip = null)}>
        <span class="dot" class:home={p.cc === home} style="width: {6 + Math.round(10 * Math.sqrt(p.visitors / max))}px; height: {6 + Math.round(10 * Math.sqrt(p.visitors / max))}px"></span>
      </Marker>
    {/each}
  </Map>
  {#if points.length === 0}
    <div class="empty">No visitor locations yet</div>
  {/if}
</div>

{#if tip}
  <div class="tip" style="left: {tip.x}px; top: {tip.y}px">
    <div class="head"><span>{flag(tip.cc)}</span><b>{countryName(tip.cc)}</b></div>
    <div class="divider"></div>
    <div class="row"><span>Visitors</span><b>{fmtNum(tip.visitors)}</b></div>
  </div>
{/if}

<style>
  .wrap { position: relative; height: 340px; border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); overflow: hidden; background: var(--up-bg); }
  .dot { display: block; border-radius: 50%; background: var(--up-accent); box-shadow: 0 0 0 2px var(--up-bg); opacity: 0.9; cursor: default; transition: transform 120ms ease-out; }
  .dot:hover { transform: scale(1.15); }
  .dot.home { background: var(--up-ink); }
  .empty { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; font: var(--up-type-meta); color: var(--up-text-faint); pointer-events: none; }
  .tip {
    position: fixed;
    transform: translate(-50%, calc(-100% - 12px));
    z-index: 100;
    pointer-events: none;
    background: var(--up-surface-dark);
    color: var(--up-text-on-dark);
    border: 1px solid var(--up-divider-on-dark);
    border-radius: var(--up-radius-tooltip);
    padding: 12px 14px;
    width: 170px;
    box-shadow: var(--up-shadow-tooltip);
  }
  .head { display: flex; align-items: center; gap: 8px; font: var(--up-type-small); }
  .head b { font-weight: 700; }
  .divider { height: 1px; background: var(--up-divider-on-dark); margin: 9px 0; }
  .row { display: flex; justify-content: space-between; font: var(--up-type-small); }
  .row span { color: var(--up-text-on-dark-muted); font-weight: 500; }
</style>
