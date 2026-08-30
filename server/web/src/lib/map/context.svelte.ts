import { getContext } from 'svelte'
import type MapLibreGL from 'maplibre-gl'

type MapContext = {
  getMap: () => MapLibreGL.Map | null
  isLoaded: () => boolean
  theme: () => 'light' | 'dark'
}

/** Reactive access to the enclosing <Map>. */
export function useMap() {
  const ctx = getContext<MapContext>('map')
  const map = $derived.by(() => ctx?.getMap() ?? null)
  const isLoaded = $derived.by(() => ctx?.isLoaded() ?? false)
  const theme = $derived.by(() => ctx?.theme() ?? 'light')
  return {
    get map() {
      return map
    },
    get isLoaded() {
      return isLoaded
    },
    get theme() {
      return theme
    },
  }
}

/** Read a CSS custom property from the document, for MapLibre paint values. */
export function cssVar(name: string, fallback: string): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}
