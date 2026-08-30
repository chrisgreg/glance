<script lang="ts">
  // The hero chart: a full-bleed smoothed spline with a fading fill, referrer
  // favicons pinned above the buckets they drove, a dotted axis with a pill on
  // the hovered bucket, and a crosshair that dims what came before it.
  import type { Marker, Point } from '../api'
  import { refIconURL } from '../api'
  import { fmtMoney, fmtNum, fmtPoint } from '../format'
  import Icon from './Icon.svelte'
  import { Tween } from 'svelte/motion'
  import { cubicOut } from 'svelte/easing'
  import { dur } from '../motion'

  let {
    series,
    markers = [],
    bucket,
    range,
    revenue = [],
    currency = '',
  }: { series: Point[]; markers?: Marker[]; bucket: 'hour' | 'day'; range: string; revenue?: number[]; currency?: string } = $props()

  const H = 220
  const PAD_TOP = 44 // room for favicons
  let width = $state(800)
  let el = $state<HTMLDivElement | null>(null)
  let hover = $state<number | null>(null)

  $effect(() => {
    if (!el) return
    const ro = new ResizeObserver(() => (width = el!.clientWidth))
    ro.observe(el)
    width = el.clientWidth
    return () => ro.disconnect()
  })

  const n = $derived(series.length)
  const max = $derived(Math.max(1, ...series.map((p) => p.visitors)))
  // Revenue bars sit behind the spline on an independent scale, so a
  // single sale never squashes the traffic line.
  const hasRevenue = $derived(revenue.length === n && revenue.some((v) => v > 0))
  const revMax = $derived(Math.max(1, ...revenue))
  const ry = (v: number) => H - 1 - (v / revMax) * (H - PAD_TOP - 1) * 0.85
  const x = (i: number) => (n <= 1 ? 0 : (i / (n - 1)) * width)
  const y = (v: number) => H - 1 - (v / max) * (H - PAD_TOP - 1)

  // Uniform cubic B-spline (the shape of d3's curveBasis): soft bells with
  // gentle shoulders, never overshooting below the baseline. The first and
  // last points are pinned so the ends meet the data.
  function spline(pts: [number, number][]): string {
    if (pts.length === 0) return ''
    if (pts.length === 1) return `M 0 ${pts[0][1]} L ${width} ${pts[0][1]}`
    // Duplicate the ends so the spline starts and finishes on the real values.
    const P = [pts[0], pts[0], ...pts, pts[pts.length - 1], pts[pts.length - 1]]
    let d = `M ${pts[0][0].toFixed(1)} ${pts[0][1].toFixed(1)}`
    for (let i = 1; i < P.length - 2; i++) {
      const [p0, p1, p2, p3] = [P[i - 1], P[i], P[i + 1], P[i + 2]]
      const c1x = (2 * p1[0] + p2[0]) / 3
      const c1y = (2 * p1[1] + p2[1]) / 3
      const c2x = (p1[0] + 2 * p2[0]) / 3
      const c2y = (p1[1] + 2 * p2[1]) / 3
      const ex = (p1[0] + 4 * p2[0] + p3[0]) / 6
      const ey = (p1[1] + 4 * p2[1] + p3[1]) / 6
      void p0
      d += ` C ${c1x.toFixed(1)} ${c1y.toFixed(1)} ${c2x.toFixed(1)} ${c2y.toFixed(1)} ${ex.toFixed(1)} ${ey.toFixed(1)}`
    }
    return d
  }
  const line = $derived(spline(series.map((p, i) => [x(i), y(p.visitors)])))
  const area = $derived(line ? `${line} L ${width} ${H} L 0 ${H} Z` : '')
  // Revenue is a second spline on its own scale, so one sale never
  // squashes the traffic line.
  const revLine = $derived(hasRevenue ? spline(revenue.map((v, i) => [x(i), ry(v)])) : '')
  const revArea = $derived(revLine ? `${revLine} L ${width} ${H} L 0 ${H} Z` : '')

  // Axis dots: every bucket for ≤ 48 points, otherwise thinned to ~24.
  const step = $derived(Math.max(1, Math.round(n / 24)))
  const markerIdx = $derived(
    markers
      .map((m) => ({ ...m, i: series.findIndex((p) => p.t === m.t) }))
      .filter((m) => m.i >= 0),
  )

  function move(e: MouseEvent) {
    if (!el || n === 0) return
    const r = el.getBoundingClientRect()
    const rel = (e.clientX - r.left) / r.width
    hover = Math.max(0, Math.min(n - 1, Math.round(rel * (n - 1))))
  }
  // The crosshair glides between buckets rather than jumping.
  const glide = new Tween(0, { duration: dur(160), easing: cubicOut })
  $effect(() => {
    if (hover !== null) glide.set(x(hover))
  })
  const hx = $derived(glide.current)
  const hp = $derived(hover === null ? null : series[hover])
  const hm = $derived(hover === null ? null : (markerIdx.find((m) => m.i === hover) ?? null))
  // Keep the tooltip inside the chart.
  const tipLeft = $derived(hx + 200 > width ? hx - 200 : hx + 14)
  const uid = Math.random().toString(36).slice(2)
</script>

<div class="wrap">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="chart" bind:this={el} onmousemove={move} onmouseleave={() => (hover = null)}>
    <svg viewBox="0 0 {width} {H}" width={width} height={H} preserveAspectRatio="none" aria-hidden="true">
      <defs>
        <linearGradient id="fill-{uid}" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stop-color="var(--up-accent-line)" stop-opacity="0.22" />
          <stop offset="100%" stop-color="var(--up-accent-line)" stop-opacity="0" />
        </linearGradient>
        <linearGradient id="revfill-{uid}" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stop-color="var(--up-revenue)" stop-opacity="0.14" />
          <stop offset="100%" stop-color="var(--up-revenue)" stop-opacity="0" />
        </linearGradient>
        <clipPath id="future-{uid}"><rect x={hover === null ? 0 : hx} y="0" width={width} height={H} /></clipPath>
      </defs>
      {#if hasRevenue}
        <path d={revArea} fill="url(#revfill-{uid})" />
        <path d={revLine} fill="none" stroke="var(--up-revenue)" stroke-width="1.6" stroke-linejoin="round" stroke-linecap="round" opacity={hover === null ? 0.9 : 0.35} />
        {#if hover !== null}
          <path d={revLine} fill="none" stroke="var(--up-revenue)" stroke-width="1.6" stroke-linejoin="round" stroke-linecap="round" clip-path="url(#future-{uid})" />
        {/if}
      {/if}
      {#each markerIdx as m (m.t)}
        <line x1={x(m.i)} x2={x(m.i)} y1={PAD_TOP - 14} y2={y(series[m.i].visitors)} stroke="var(--up-border-control)" stroke-width="1" stroke-dasharray="2 4" />
      {/each}
      <path d={area} fill="url(#fill-{uid})" />
      <!-- past: dimmed; future (from the crosshair onwards): full -->
      <path d={line} fill="none" stroke="var(--up-accent-line)" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" opacity={hover === null ? 1 : 0.35} />
      {#if hover !== null}
        <path d={line} fill="none" stroke="var(--up-accent-line)" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" clip-path="url(#future-{uid})" />
        <line x1={hx} x2={hx} y1={PAD_TOP - 20} y2={H} stroke="var(--up-ink)" stroke-width="1.2" />
      {/if}
    </svg>

    {#each markerIdx as m (m.t)}
      <div class="marker" style="left: {x(m.i)}px" title="{m.ref} · {fmtNum(m.visitors)} visitors">
        <Icon src={refIconURL(m.ref)} size={20} />
      </div>
    {/each}

    {#if hp}
      <div class="tip" style="left: {tipLeft}px">
        <div class="when">{fmtPoint(hp.t, bucket, range)}</div>
        <div class="row"><span><i class="dot people"></i>People</span><b>{fmtNum(hp.visitors)}</b></div>
        <div class="row"><span><i class="dot views"></i>Views</span><b>{fmtNum(hp.pageviews)}</b></div>
        {#if hasRevenue}
          <div class="row"><span><i class="dot revenue"></i>Revenue</span><b>{fmtMoney(revenue[hover!] ?? 0, currency)}</b></div>
        {/if}
        {#if hm}
          <div class="divider"></div>
          <div class="when">Notable referrer</div>
          <div class="row"><span><Icon src={refIconURL(hm.ref)} size={14} />{hm.ref}</span><b>{fmtNum(hm.visitors)}</b></div>
        {/if}
      </div>
    {/if}
  </div>

  <div class="axis" style="width: {width}px">
    <span class="edge">{series[0] ? fmtPoint(series[0].t, bucket, range) : ''}</span>
    <div class="dots">
      {#each series as p, i (p.t)}
        {#if i % step === 0 || i === n - 1}
          <span class="tick" class:on={hover === i} style="left: {x(i)}px"></span>
        {/if}
      {/each}
      {#if hp}
        <span class="pill" style="left: {hx}px">{fmtPoint(hp.t, bucket, range)}</span>
      {/if}
    </div>
    <span class="edge">{range === '24h' ? 'Now' : 'Today'}</span>
  </div>
</div>

<style>
  /* Break out of the content column to the viewport edges, like the reference. */
  .wrap { width: 100vw; margin-left: calc(50% - 50vw); display: flex; flex-direction: column; gap: 10px; }
  .chart { position: relative; width: 100%; height: 220px; }
  svg { display: block; overflow: visible; }
  .marker { position: absolute; top: 8px; transform: translateX(-50%); pointer-events: none; }
  .tip {
    position: absolute; top: 10px;
    background: var(--up-surface-dark); color: var(--up-text-on-dark);
    border: 1px solid var(--up-divider-on-dark); border-radius: var(--up-radius-tooltip);
    padding: 10px 12px; width: 176px; box-shadow: var(--up-shadow-tooltip); pointer-events: none;
    display: flex; flex-direction: column; gap: 6px;
  }
  .when { font: var(--up-type-small); font-weight: 500; color: var(--up-text-on-dark-muted); }
  .row { display: flex; justify-content: space-between; font: var(--up-type-small); }
  .row span { display: inline-flex; align-items: center; gap: 7px; font-weight: 500; }
  .dot { width: 7px; height: 7px; border-radius: 50%; display: inline-block; }
  .dot.people { background: var(--up-accent-line); }
  .dot.views { background: var(--up-text-on-dark-muted); }
  .dot.revenue { background: var(--up-revenue); }
  .axis { position: relative; display: flex; align-items: center; justify-content: space-between; height: 20px; padding: 0 var(--up-page-pad); box-sizing: border-box; }
  .edge { font: var(--up-type-caption); color: var(--up-text-muted); background: var(--up-bg); position: relative; z-index: 1; padding: 0 4px; }
  .dots { position: absolute; left: 0; right: 0; top: 0; height: 20px; }
  .tick { position: absolute; top: 8px; width: 4px; height: 4px; margin-left: -2px; border-radius: 50%; background: var(--up-border-control); }
  .tick.on { background: var(--up-ink); }
  .pill { position: absolute; top: 0; transform: translateX(-50%); background: var(--up-ink); color: var(--up-bg); font: var(--up-type-caption); font-weight: 600; padding: 3px 8px; border-radius: var(--up-radius-pill); white-space: nowrap; z-index: 2; }
  .divider { height: 1px; background: var(--up-divider-on-dark); margin: 2px 0; }
  @media (max-width: 600px) {
    .chart { height: 180px; }
  }
</style>
