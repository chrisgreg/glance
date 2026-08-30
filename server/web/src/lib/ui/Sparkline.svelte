<script lang="ts">
  // Cubic-smoothed periwinkle line, 96×28 next to list rows.
  let { values, width = 96, height = 28 }: { values: number[]; width?: number; height?: number } = $props()

  const path = $derived.by(() => {
    if (values.length < 2) return ''
    const max = Math.max(...values, 1)
    const px = (i: number) => (i / (values.length - 1)) * width
    const py = (v: number) => height - 3 - (v / max) * (height - 6)
    let d = `M ${px(0).toFixed(1)} ${py(values[0]).toFixed(1)}`
    for (let i = 1; i < values.length; i++) {
      const x0 = px(i - 1)
      const x1 = px(i)
      const xm = (x0 + x1) / 2
      d += ` C ${xm.toFixed(1)} ${py(values[i - 1]).toFixed(1)} ${xm.toFixed(1)} ${py(values[i]).toFixed(1)} ${x1.toFixed(1)} ${py(values[i]).toFixed(1)}`
    }
    return d
  })
</script>

{#if path}
  <svg viewBox="0 0 {width} {height}" {width} {height} aria-hidden="true">
    <path d={path} fill="none" stroke="var(--up-accent-line)" stroke-width="1.8" stroke-linejoin="round" stroke-linecap="round" />
  </svg>
{/if}

<style>
  svg { flex-shrink: 0; display: block; }
</style>
