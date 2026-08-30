<script lang="ts">
  // "N people in the last 30m" with a 30-column per-minute strip along the
  // bottom edge. Bars grow in and re-scale as new minutes arrive.
  import { fmtNum } from '../format'
  let { minutes, total, onmore }: { minutes: number[]; total: number; onmore?: () => void } = $props()
  const max = $derived(Math.max(1, ...minutes))
  const now = $derived(minutes[minutes.length - 1] ?? 0)
</script>

<div class="card">
  <div class="head">
    <div class="count"><b>{fmtNum(total)}</b> {total === 1 ? 'person' : 'people'} <span class="muted">in the last 30m</span></div>
    {#if onmore}<button type="button" class="pill" onclick={onmore}>Realtime <span class="chev">›</span></button>{/if}
  </div>
  <div class="strip" aria-hidden="true">
    {#each minutes as v, i (i)}
      <div class="bar" class:on={v > 0} class:last={i === minutes.length - 1} style="height: {v > 0 ? 6 + Math.round(24 * (v / max)) : 6}px" title="{v} {v === 1 ? 'visitor' : 'visitors'}"></div>
    {/each}
  </div>
  <div class="foot"><span>30 minutes ago</span><span>{now > 0 ? `${now} right now` : 'Now'}</span></div>
</div>

<style>
  .card { border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); padding: 20px 20px 12px; display: flex; flex-direction: column; gap: 14px; min-width: 0; }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .count { font: var(--up-type-row-title); }
  .count b { font-weight: 700; }
  .muted { color: var(--up-text-muted); font-weight: 500; }
  .pill { font: var(--up-type-ui); color: var(--up-text-muted); background: var(--up-bg-hover); border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-pill); padding: 4px 10px 4px 12px; cursor: pointer; display: inline-flex; align-items: center; gap: 4px; }
  .pill:hover { color: var(--up-ink); }
  .chev { font-size: 14px; line-height: 1; }
  .strip { display: flex; align-items: flex-end; gap: 3px; height: 32px; margin-top: 8px; }
  .bar { flex: 1; border-radius: 2px; background: var(--up-nodata); transform-origin: bottom; transition: height 400ms cubic-bezier(0.2, 0, 0, 1), background 200ms ease-out; animation: rise 500ms cubic-bezier(0.2, 0, 0, 1) both; }
  .bar.on { background: var(--up-operational); }
  .bar.last.on { background: var(--up-accent); }
  @keyframes rise { from { transform: scaleY(0); } to { transform: scaleY(1); } }
  @media (prefers-reduced-motion: reduce) { .bar { animation: none; } }
  .foot { display: flex; justify-content: space-between; font: var(--up-type-caption); color: var(--up-text-faint); }
</style>
