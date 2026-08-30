<script lang="ts">
  // Ported from mapcn-svelte (MIT) with SvelteKit and Tailwind removed. A
  // tile-less MapLibre canvas; children add layers via the "map" context.
  import { onMount, setContext } from 'svelte'
  import MapLibreGL from 'maplibre-gl'
  import 'maplibre-gl/dist/maplibre-gl.css'

  const blankStyle: MapLibreGL.StyleSpecification = {
    version: 8,
    sources: {},
    layers: [{ id: 'background', type: 'background', paint: { 'background-color': 'rgba(0,0,0,0)' } }],
  }

  interface Props {
    children?: import('svelte').Snippet
    center?: [number, number]
    zoom?: number
    options?: Omit<MapLibreGL.MapOptions, 'container' | 'style'>
    map?: MapLibreGL.Map | null
    /** 'globe' renders a 3D globe (no tiles needed). */
    projection?: 'mercator' | 'globe'
  }
  let { children, center = [10, 20], zoom = 1, options = {}, map = $bindable(null), projection = 'mercator' }: Props = $props()

  let container: HTMLDivElement
  let isLoaded = $state(false)
  let theme = $state<'light' | 'dark'>('light')

  function readTheme(): 'light' | 'dark' {
    const t = document.documentElement.dataset.theme
    if (t === 'dark' || t === 'light') return t
    return matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }

  setContext('map', {
    getMap: () => map,
    isLoaded: () => isLoaded,
    theme: () => theme,
  })

  onMount(() => {
    theme = readTheme()
    const observer = new MutationObserver(() => (theme = readTheme()))
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
    const mq = matchMedia('(prefers-color-scheme: dark)')
    const onMq = () => (theme = readTheme())
    mq.addEventListener('change', onMq)

    const instance = new MapLibreGL.Map({
      container,
      style: blankStyle,
      center,
      zoom,
      fadeDuration: 0,
      renderWorldCopies: false,
      attributionControl: false,
      ...options,
    })
    instance.on('load', () => {
      instance.resize()
      if (projection === 'globe') instance.setProjection({ type: 'globe' })
      isLoaded = true
    })
    // The container may get its final size after MapLibre measured it.
    const ro = new ResizeObserver(() => instance.resize())
    ro.observe(container)
    map = instance
    return () => {
      ro.disconnect()
      observer.disconnect()
      mq.removeEventListener('change', onMq)
      instance.remove()
      map = null
      isLoaded = false
    }
  })
</script>

<div class="map" bind:this={container}>
  {#if map}
    {@render children?.()}
  {/if}
</div>

<style>
  .map { position: relative; width: 100%; height: 100%; }
  .map :global(.maplibregl-canvas) { outline: none; }
  .map :global(.maplibregl-ctrl-logo) { display: none; }
</style>
