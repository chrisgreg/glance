<script lang="ts">
  // Browser, OS and device icons for breakdown rows. Brand marks come from
  // SVGL (svgl.app) and are bundled at build time, so nothing is fetched at
  // runtime. Devices use small stroke glyphs in the design's icon style.
  import chrome from '../icons/chrome.svg'
  import safari from '../icons/safari.svg'
  import firefox from '../icons/firefox.svg'
  import edge from '../icons/edge.svg'
  import opera from '../icons/opera.svg'
  import brave from '../icons/brave.svg'
  import arc from '../icons/arc_browser.svg'
  import vivaldi from '../icons/vivaldi.svg'
  import duckduckgo from '../icons/duckduckgo.svg'
  import windows from '../icons/windows.svg'
  import apple from '../icons/apple.svg'
  import appleDark from '../icons/apple_dark.svg'
  import android from '../icons/android-icon.svg'
  import linux from '../icons/linux.svg'

  let { kind, name, size = 18 }: { kind: 'browser' | 'os' | 'device'; name: string; size?: number } = $props()

  const browsers: Record<string, string> = { Chrome: chrome, Chromium: chrome, Safari: safari, Firefox: firefox, Edge: edge, Opera: opera, Brave: brave, Arc: arc, Vivaldi: vivaldi, DuckDuckGo: duckduckgo }
  const oses: Record<string, string> = { Windows: windows, macOS: apple, iOS: apple, iPadOS: apple, Android: android, Linux: linux, ChromeOS: chrome }
  const src = $derived(kind === 'browser' ? browsers[name] : kind === 'os' ? oses[name] : undefined)
  const isApple = $derived(kind === 'os' && src === apple)
</script>

{#if kind === 'device'}
  <span class="glyph" style="width: {size}px; height: {size}px">
    {#if name === 'Mobile'}
      <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="1" width="6" height="10" rx="1.4" /><path d="M5.3 9.3h1.4" /></svg>
    {:else if name === 'Tablet'}
      <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><rect x="1.5" y="1" width="9" height="10" rx="1.4" /><path d="M5.3 9.3h1.4" /></svg>
    {:else}
      <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><rect x="1" y="1.5" width="10" height="7" rx="1.2" /><path d="M4 11h4M6 8.5V11" /></svg>
    {/if}
  </span>
{:else if src}
  {#if isApple}
    <img class="brand light-only" src={apple} alt="" width={size} height={size} />
    <img class="brand dark-only" src={appleDark} alt="" width={size} height={size} />
  {:else}
    <img class="brand" {src} alt="" width={size} height={size} />
  {/if}
{:else}
  <span class="glyph blank" style="width: {size}px; height: {size}px"></span>
{/if}

<style>
  .brand { display: block; flex-shrink: 0; object-fit: contain; }
  .glyph { display: inline-flex; align-items: center; justify-content: center; border-radius: 50%; background: var(--up-bg); box-shadow: var(--up-ring-inset); color: var(--up-text-muted); flex-shrink: 0; }
  .glyph.blank { background: var(--up-accent-tint); }
  .dark-only { display: none; }
  :global(:root[data-theme='dark']) .light-only { display: none; }
  :global(:root[data-theme='dark']) .dark-only { display: block; }
  @media (prefers-color-scheme: dark) {
    :global(:root:not([data-theme='light'])) .light-only { display: none; }
    :global(:root:not([data-theme='light'])) .dark-only { display: block; }
  }
</style>
