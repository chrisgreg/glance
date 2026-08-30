<script lang="ts">
  // Ported from mapcn-svelte's MapArc (MIT): quadratic-bezier arcs in
  // lng/lat space, unwrapped across the antimeridian. Width follows weight.
  import { untrack } from 'svelte'
  import type MapLibreGL from 'maplibre-gl'
  import { useMap, cssVar } from './context.svelte'

  export interface Arc {
    id: string
    from: [number, number]
    to: [number, number]
    weight: number // 0..1
  }
  let { arcs, curvature = 0.25, samples = 48, animated = false, id = 'arcs' }: { arcs: Arc[]; curvature?: number; samples?: number; animated?: boolean; id?: string } = $props()
  const ctx = useMap()
  // Intentionally fixed for the component's lifetime.
  const sourceId = untrack(() => id)
  const layerId = sourceId + '-line'

  function coords(from: [number, number], to: [number, number]): [number, number][] {
    const [x0, y0] = from
    const [xt, y2] = to
    const dxRaw = xt - x0
    const x2 = dxRaw > 180 ? xt - 360 : dxRaw < -180 ? xt + 360 : xt
    const dx = x2 - x0
    const dy = y2 - y0
    const dist = Math.hypot(dx, dy)
    if (dist === 0 || curvature === 0) return [from, [x2, y2]]
    const mx = (x0 + x2) / 2
    const my = (y0 + y2) / 2
    const nx = -dy / dist
    const ny = dx / dist
    const off = dist * curvature
    const cx = mx + nx * off
    const cy = my + ny * off
    const pts: [number, number][] = []
    for (let i = 0; i <= samples; i++) {
      const t = i / samples
      const inv = 1 - t
      pts.push([inv * inv * x0 + 2 * inv * t * cx + t * t * x2, inv * inv * y0 + 2 * inv * t * cy + t * t * y2])
    }
    return pts
  }

  const geo = $derived<GeoJSON.FeatureCollection<GeoJSON.LineString>>({
    type: 'FeatureCollection',
    features: arcs.map((a) => ({
      type: 'Feature',
      properties: { id: a.id, weight: a.weight },
      geometry: { type: 'LineString', coordinates: coords(a.from, a.to) },
    })),
  })

  $effect(() => {
    const map = ctx.map
    if (!map || !ctx.isLoaded) return
    const initial = untrack(() => geo)
    if (!map.getSource(sourceId)) {
      map.addSource(sourceId, { type: 'geojson', data: initial })
      map.addLayer({
        id: layerId,
        type: 'line',
        source: sourceId,
        layout: { 'line-join': 'round', 'line-cap': 'round' },
        paint: {
          'line-color': cssVar('--up-accent-line', '#8B92EC'),
          'line-width': ['interpolate', ['linear'], ['get', 'weight'], 0, 1, 1, 3.5],
          'line-opacity': 0.85,
          'line-dasharray': [2, 2],
        },
      })
    }
    return () => {
      try {
        if (map.getLayer(layerId)) map.removeLayer(layerId)
        if (map.getSource(sourceId)) map.removeSource(sourceId)
      } catch {}
    }
  })

  $effect(() => {
    const map = ctx.map
    if (!map || !ctx.isLoaded) return
    const src = map.getSource(sourceId) as MapLibreGL.GeoJSONSource | undefined
    if (src) src.setData(geo)
  })

  $effect(() => {
    const map = ctx.map
    const theme = ctx.theme
    if (!map || !ctx.isLoaded || !map.getLayer(layerId)) return
    void theme
    map.setPaintProperty(layerId, 'line-color', cssVar('--up-accent-line', '#8B92EC'))
  })

  // Marching dashes: cycling through shifted dash patterns reads as flow
  // along the arc from source to destination. Off under reduced motion.
  const dashSteps: [number, number][] = [
    [0, 4, 3],
    [0.5, 4, 2.5],
    [1, 4, 2],
    [1.5, 4, 1.5],
    [2, 4, 1],
    [2.5, 4, 0.5],
    [3, 4, 0],
    [0, 0.5, 3, 3.5],
    [0, 1, 3, 3],
    [0, 1.5, 3, 2.5],
    [0, 2, 3, 2],
    [0, 2.5, 3, 1.5],
    [0, 3, 3, 1],
    [0, 3.5, 3, 0.5],
  ] as unknown as [number, number][]
  $effect(() => {
    const map = ctx.map
    if (!map || !ctx.isLoaded || !animated) return
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) return
    let step = 0
    let raf = 0
    let last = 0
    const tick = (t: number) => {
      if (t - last > 70) {
        last = t
        step = (step + 1) % dashSteps.length
        if (map.getLayer(layerId)) map.setPaintProperty(layerId, 'line-dasharray', dashSteps[step])
      }
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  })
</script>
