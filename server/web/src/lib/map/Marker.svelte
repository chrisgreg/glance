<script lang="ts">
  // A MapLibre marker whose content is rendered by Svelte. Ported from
  // mapcn-svelte's MapMarker + MarkerContent (MIT).
  import { untrack } from 'svelte'
  import MapLibreGL from 'maplibre-gl'
  import { useMap } from './context.svelte'

  let {
    lng,
    lat,
    children,
    onenter,
    onleave,
  }: { lng: number; lat: number; children?: import('svelte').Snippet; onenter?: (el: HTMLElement) => void; onleave?: () => void } = $props()

  const ctx = useMap()
  let holder = $state<HTMLDivElement | null>(null)
  let marker: MapLibreGL.Marker | null = null

  $effect(() => {
    const map = ctx.map
    if (!map || !ctx.isLoaded || !holder) return
    const el = document.createElement('div')
    const content = untrack(() => holder!)
    const nodes = Array.from(content.childNodes)
    nodes.forEach((n) => el.appendChild(n))
    const enter = () => onenter?.(el)
    const leave = () => onleave?.()
    el.addEventListener('mouseenter', enter)
    el.addEventListener('mouseleave', leave)
    marker = new MapLibreGL.Marker({ element: el, anchor: 'center' }).setLngLat(untrack(() => [lng, lat])).addTo(map)
    return () => {
      el.removeEventListener('mouseenter', enter)
      el.removeEventListener('mouseleave', leave)
      marker?.remove()
      marker = null
      nodes.forEach((n) => content.appendChild(n))
    }
  })

  $effect(() => {
    marker?.setLngLat([lng, lat])
  })
</script>

<div bind:this={holder} style="display: contents">
  {@render children?.()}
</div>
