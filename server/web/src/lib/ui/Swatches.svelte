<script lang="ts">
  // Accent picker: the preset colours, a hex field for anything else, and —
  // when a site can defer to the account-wide colour — a way back to it.
  import { isHex, SWATCHES } from '../accent'
  import Input from './Input.svelte'

  let {
    value,
    onpick,
    inherit = '',
  }: {
    value: string
    onpick: (hex: string) => void
    /** Label for the "use the account-wide accent" option; omitted = no such option. */
    inherit?: string
  } = $props()

  // The hex field carries anything that is not one of the presets, until it
  // is typed in, after which it shows what was typed.
  let typed = $state<string | null>(null)
  const preset = $derived(SWATCHES.some((s) => s.hex === value.toUpperCase()))
  const custom = $derived(typed ?? (preset ? '' : value))
  function type(v: string) {
    typed = v.trim()
    if (isHex(typed)) onpick(typed.toUpperCase())
  }
</script>

<div class="swatches">
  {#if inherit}
    <button type="button" class="reset" class:on={!value} onclick={() => { typed = ''; onpick('') }}>{inherit}</button>
  {/if}
  {#each SWATCHES as sw (sw.hex)}
    <button
      type="button"
      class="swatch"
      class:on={value.toUpperCase() === sw.hex}
      title={sw.name}
      aria-label={sw.name}
      style="background: {sw.hex}; --ring: {sw.hex}"
      onclick={() => { typed = ''; onpick(sw.hex) }}
    ></button>
  {/each}
  <div class="custom">
    <Input value={custom} placeholder="#7C83E8" aria-label="Custom accent" maxlength={7} oninput={(e) => type(e.currentTarget.value)} />
  </div>
</div>

<style>
  .swatches { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; justify-content: flex-end; }
  .swatch { width: 22px; height: 22px; border-radius: 50%; border: none; cursor: pointer; box-shadow: var(--up-ring-inset); transition: transform 120ms ease-out; }
  .swatch:hover { transform: scale(1.1); }
  .swatch.on { box-shadow: 0 0 0 2px var(--up-bg), 0 0 0 3.5px var(--ring); }
  .reset { background: none; border: none; padding: 0; cursor: pointer; font: var(--up-type-ui); color: var(--up-text-inactive); }
  .reset:hover { color: var(--up-ink); }
  .reset.on { color: var(--up-ink); }
  .custom { width: 110px; margin-left: 6px; }
  @media (prefers-reduced-motion: reduce) {
    .swatch { transition: none; }
    .swatch:hover { transform: none; }
  }
</style>
