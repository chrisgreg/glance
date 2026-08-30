<script lang="ts">
  // Realtime globe: visitors from the last five minutes, arcs flowing from
  // each country to the home country, pulsing dots sized by count, and a
  // slow auto-rotation that pauses while the user is interacting.
  import Map from './Map.svelte'
  import Land from './Land.svelte'
  import Arcs, { type Arc } from './Arcs.svelte'
  import Marker from './Marker.svelte'
  import type MapLibreGL from 'maplibre-gl'
  import type { Live } from '../api'
  import { countryCentroid, countryName, flag, fmtNum } from '../format'

  let { live, home }: { live: Live; home: string } = $props()
  let map = $state<MapLibreGL.Map | null>(null)

  const points = $derived(
    live.countries
      .map((r) => ({ cc: r.key, visitors: r.visitors, at: countryCentroid(r.key) }))
      .filter((p): p is { cc: string; visitors: number; at: [number, number] } => p.at !== null),
  )
  const max = $derived(Math.max(1, ...points.map((p) => p.visitors)))
  const hub = $derived(countryCentroid(home) ?? points[0]?.at ?? null)
  const arcs = $derived<Arc[]>(hub ? points.filter((p) => p.cc !== home).map((p) => ({ id: p.cc, from: p.at, to: hub!, weight: p.visitors / max })) : [])
  let tip = $state<{ cc: string; visitors: number; x: number; y: number } | null>(null)
  function enter(p: { cc: string; visitors: number }, el: HTMLElement) {
    const r = el.getBoundingClientRect()
    tip = { cc: p.cc, visitors: p.visitors, x: r.left + r.width / 2, y: r.top }
  }

  // Auto-rotate: nudge the centre longitude each frame unless the user is
  // dragging or zooming; resume a few seconds after they stop.
  $effect(() => {
    const m = map
    if (!m || matchMedia('(prefers-reduced-motion: reduce)').matches) return
    let raf = 0
    let paused = false
    let resume: ReturnType<typeof setTimeout> | undefined
    const pause = () => {
      paused = true
      clearTimeout(resume)
      resume = setTimeout(() => (paused = false), 4000)
    }
    m.on('mousedown', pause)
    m.on('touchstart', pause)
    m.on('wheel', pause)
    const spin = () => {
      if (!paused && !m.isMoving()) {
        const c = m.getCenter()
        m.setCenter([c.lng + 0.06, c.lat])
      }
      raf = requestAnimationFrame(spin)
    }
    raf = requestAnimationFrame(spin)
    return () => {
      cancelAnimationFrame(raf)
      clearTimeout(resume)
      m.off('mousedown', pause)
      m.off('touchstart', pause)
      m.off('wheel', pause)
    }
  })
</script>

<div class="wrap">
  <Map bind:map projection="globe" center={hub ?? [0, 20]} zoom={1.4} options={{ scrollZoom: false, dragRotate: false, pitchWithRotate: false, minZoom: 1, maxZoom: 4 }}>
    <Land />
    <Arcs {arcs} animated id="live-arcs" />
    {#each points as p (p.cc)}
      <Marker lng={p.at[0]} lat={p.at[1]} onenter={(el) => enter(p, el)} onleave={() => (tip = null)}>
        <span class="pulse" class:home={p.cc === home} style="--size: {8 + Math.round(10 * Math.sqrt(p.visitors / max))}px"><span class="core"></span></span>
      </Marker>
    {/each}
    {#if hub}
      <Marker lng={hub[0]} lat={hub[1]}>
        <span class="hubwrap">
          {#if !points.some((p) => p.cc === home)}<span class="hubdot"></span>{/if}
          <span class="hublabel">{countryName(home) || 'Home'}</span>
        </span>
      </Marker>
    {/if}
  </Map>
  <div class="overlay">
    <div class="count"><span class="dot" class:on={live.total > 0}></span>{live.total} {live.total === 1 ? 'visitor' : 'visitors'} in the last 5 minutes</div>
    {#if points.length}
      <div class="list">
        {#each points.slice(0, 6) as p (p.cc)}
          <span class="chip">{flag(p.cc)} {countryName(p.cc)} <b>{fmtNum(p.visitors)}</b></span>
        {/each}
      </div>
    {/if}
  </div>
</div>

{#if tip}
  <div class="tip" style="left: {tip.x}px; top: {tip.y}px">
    <div class="head"><span>{flag(tip.cc)}</span><b>{countryName(tip.cc)}</b></div>
    <div class="divider"></div>
    <div class="row"><span>Online now</span><b>{fmtNum(tip.visitors)}</b></div>
  </div>
{/if}

<style>
  .wrap { position: relative; height: 420px; border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); overflow: hidden; background: var(--up-bg); }
  .overlay { position: absolute; left: 14px; right: 14px; bottom: 12px; display: flex; flex-direction: column; gap: 8px; pointer-events: none; }
  .count { display: flex; align-items: center; gap: 8px; font: var(--up-type-meta); color: var(--up-text-secondary); }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--up-text-faint); }
  .dot.on { background: var(--up-accent); box-shadow: 0 0 0 3px var(--up-accent-tint); }
  .list { display: flex; flex-wrap: wrap; gap: 6px; }
  .chip { font: var(--up-type-small); font-weight: 500; color: var(--up-text-muted); background: var(--up-bg); border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-pill); padding: 3px 10px; }
  .chip b { color: var(--up-ink); font-weight: 600; margin-left: 2px; }

  .pulse { position: relative; display: block; width: var(--size); height: var(--size); }
  .pulse::before { content: ''; position: absolute; inset: 0; border-radius: 50%; background: var(--up-accent); opacity: 0.35; animation: ring 2.2s ease-out infinite; }
  .core { position: absolute; inset: 25%; border-radius: 50%; background: var(--up-accent); box-shadow: 0 0 0 2px var(--up-bg); }
  .pulse.home .core { background: var(--up-ink); }
  .hubwrap { position: relative; display: flex; align-items: center; justify-content: center; }
  .hubdot { display: block; width: 10px; height: 10px; border-radius: 50%; background: var(--up-ink); box-shadow: 0 0 0 2px var(--up-bg); }
  .hublabel { position: absolute; top: 12px; white-space: nowrap; font: var(--up-type-caption); font-weight: 600; color: var(--up-text-secondary); background: var(--up-bg); border: 1px solid var(--up-border-hairline); border-radius: 4px; padding: 1px 6px; }
  @keyframes ring { 0% { transform: scale(0.6); opacity: 0.5; } 100% { transform: scale(2.2); opacity: 0; } }
  @media (prefers-reduced-motion: reduce) { .pulse::before { animation: none; } }

  .tip { position: fixed; transform: translate(-50%, calc(-100% - 12px)); z-index: 100; pointer-events: none; background: var(--up-surface-dark); color: var(--up-text-on-dark); border: 1px solid var(--up-divider-on-dark); border-radius: var(--up-radius-tooltip); padding: 12px 14px; width: 170px; box-shadow: var(--up-shadow-tooltip); }
  .head { display: flex; align-items: center; gap: 8px; font: var(--up-type-small); }
  .head b { font-weight: 700; }
  .divider { height: 1px; background: var(--up-divider-on-dark); margin: 9px 0; }
  .row { display: flex; justify-content: space-between; font: var(--up-type-small); }
  .row span { color: var(--up-text-on-dark-muted); font-weight: 500; }
</style>
