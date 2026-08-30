<script lang="ts">
  // World outlines from the bundled Natural Earth 110m set (public domain),
  // so the map needs no tile server. Fill and stroke follow the theme tokens.
  import { untrack } from 'svelte'
  import { feature } from 'topojson-client'
  import type { Topology, GeometryCollection } from 'topojson-specification'
  import world from 'world-atlas/countries-110m.json'
  import { useMap, cssVar } from './context.svelte'

  const ctx = useMap()
  // Antarctica's polygon wraps the antimeridian and fills the whole canvas in
  // a flat projection, so it is left out.
  const all = feature(world as unknown as Topology, (world as any).objects.countries as GeometryCollection) as GeoJSON.FeatureCollection
  // Rings that cross the antimeridian (Russia, Fiji, the Aleutians) would
  // otherwise wrap the long way round and fill the whole canvas. Shift their
  // western longitudes past 180 so the ring stays contiguous.
  function unwrap(ring: number[][]): number[][] {
    let crosses = false
    for (let i = 1; i < ring.length; i++) if (Math.abs(ring[i][0] - ring[i - 1][0]) > 180) crosses = true
    return crosses ? ring.map(([x, y]) => [x < 0 ? x + 360 : x, y]) : ring
  }
  const geo: GeoJSON.FeatureCollection = {
    type: 'FeatureCollection',
    features: all.features
      .filter((f) => f.id !== '010' && (f.properties as any)?.name !== 'Antarctica')
      .map((f) => {
        const g = f.geometry
        if (g.type === 'Polygon') return { ...f, geometry: { type: 'Polygon', coordinates: g.coordinates.map(unwrap) } }
        if (g.type === 'MultiPolygon') return { ...f, geometry: { type: 'MultiPolygon', coordinates: g.coordinates.map((poly) => poly.map(unwrap)) } }
        return f
      }),
  }

  $effect(() => {
    const map = ctx.map
    if (!map || !ctx.isLoaded) return
    untrack(() => {
      if (!map.getSource('land')) {
        map.addSource('land', { type: 'geojson', data: geo as any })
        map.addLayer({ id: 'land-fill', type: 'fill', source: 'land', paint: { 'fill-color': cssVar('--up-accent-tint', '#EEF0FB'), 'fill-opacity': 1 } })
        map.addLayer({ id: 'land-line', type: 'line', source: 'land', paint: { 'line-color': cssVar('--up-border-control', '#E4E6EE'), 'line-width': 0.6 } })
      }
    })
    return () => {
      try {
        if (map.getLayer('land-line')) map.removeLayer('land-line')
        if (map.getLayer('land-fill')) map.removeLayer('land-fill')
        if (map.getSource('land')) map.removeSource('land')
      } catch {}
    }
  })

  // Re-read the tokens when the theme flips.
  $effect(() => {
    const map = ctx.map
    const theme = ctx.theme
    if (!map || !ctx.isLoaded || !map.getLayer('land-fill')) return
    void theme
    map.setPaintProperty('land-fill', 'fill-color', cssVar('--up-accent-tint', '#EEF0FB'))
    map.setPaintProperty('land-line', 'line-color', cssVar('--up-border-control', '#E4E6EE'))
  })
</script>
