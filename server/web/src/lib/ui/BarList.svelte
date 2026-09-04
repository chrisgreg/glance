<script lang="ts" module>
  export interface BarRow {
    key: string
    label: string
    value: number
    icon?: string // image url
    direct?: boolean
    prefix?: string // e.g. flag emoji
    title?: string
  }
</script>

<script lang="ts" generics="T extends string">
  // Breakdown card: hairline 14px card, optional underline tabs in the
  // header, rows with a pale accent-tint proportion fill that grows in when
  // rows appear or change. Also used bare inside the "view all" modal.
  import { type Snippet } from 'svelte'
  import { fade } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { flip } from 'svelte/animate'
  import { fmtNum } from '../format'
  import { dur } from '../motion'
  import Segment from './Segment.svelte'

  let {
    title = '',
    rows,
    empty = 'Nothing yet',
    icon,
    bare = false,
    onmore,
    tabs,
    tab,
    ontab,
    format = fmtNum,
    onselect,
    selected,
  }: {
    title?: string
    rows: BarRow[]
    empty?: string
    icon?: Snippet<[BarRow]>
    bare?: boolean
    format?: (v: number) => string
    /** Click a row to filter by it. Rows keyed "Other" are a fold and stay inert. */
    onselect?: (row: BarRow) => void
    selected?: string
    onmore?: () => void
    tabs?: { value: T; label: string }[]
    tab?: T
    ontab?: (v: T) => void
  } = $props()
  const top = $derived(rows.length ? Math.max(rows[0].value, 1) : 1)
  // Bump a key whenever the tab changes so every row re-mounts and its fill
  // animates open again, the way the reference does.
  const gen = $derived(String(tab ?? ''))
</script>

<div class="card" class:bare>
  {#if !bare}
    <div class="head">
      <div class="title-wrap">
        <div class="title">{title}</div>
        {#if onmore}
          <button type="button" class="expand" title="View all" aria-label="View all" onclick={onmore}>
            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M7 1.5h3.5V5M10.5 1.5 6.5 5.5M5 10.5H1.5V7M1.5 10.5l4-4" /></svg>
          </button>
        {/if}
      </div>
      {#if tabs && tab !== undefined && ontab}
        <Segment options={tabs} value={tab} gap={12} onchange={ontab} />
      {/if}
    </div>
  {/if}
  {#key gen}
    {#if rows.length === 0}
      <div class="empty" in:fade={{ duration: dur(140) }}>{empty}</div>
    {:else}
      <div class="rows">
        {#each rows as r, i (r.key)}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <svelte:element
            this={onselect && r.key !== 'Other' ? 'button' : 'div'}
            type={onselect && r.key !== 'Other' ? 'button' : undefined}
            class="row"
            class:clickable={onselect && r.key !== 'Other'}
            class:selected={selected !== undefined && selected === r.key}
            aria-pressed={onselect && r.key !== 'Other' ? selected === r.key : undefined}
            title={r.title ?? r.label}
            animate:flip={{ duration: dur(220), easing: cubicOut }}
            in:fade={{ duration: dur(160), delay: dur(Math.min(i, 10) * 25) }}
            onclick={onselect && r.key !== 'Other' ? () => onselect(r) : undefined}
          >
            <div class="fill" style="width: {Math.round((r.value / top) * 100)}%; animation-delay: {dur(Math.min(i, 10) * 25)}ms"></div>
            <div class="left">
              {#if icon}{@render icon(r)}{/if}
              {#if r.prefix}<span class="prefix">{r.prefix}</span>{/if}
              <span class="label">{r.label}</span>
            </div>
            <div class="value">{format(r.value)}</div>
          </svelte:element>
        {/each}
      </div>
    {/if}
  {/key}
</div>

<style>
  .card { border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); padding: 20px 20px 14px; display: flex; flex-direction: column; gap: 14px; min-width: 0; }
  .card.bare { border: none; padding: 0; gap: 0; }
  .head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; min-width: 0; }
  .head > :global(:last-child) { min-width: 0; overflow-x: auto; scrollbar-width: none; }
  .head > :global(:last-child::-webkit-scrollbar) { display: none; }
  .title-wrap { display: flex; align-items: center; gap: 8px; }
  .title { font: var(--up-type-setting); font-weight: 700; }
  .expand { display: inline-flex; background: none; border: none; padding: 2px; cursor: pointer; color: var(--up-text-faint); border-radius: 4px; }
  .expand:hover { color: var(--up-ink); background: var(--up-bg-hover); }
  .rows { display: flex; flex-direction: column; gap: 6px; }
  .row { position: relative; display: flex; justify-content: space-between; align-items: center; gap: 12px; padding: 6px 10px; border-radius: var(--up-radius-row, 6px); overflow: hidden; width: 100%; text-align: left; background: none; border: none; color: inherit; font: inherit; }
  .row.clickable { cursor: pointer; }
  .row.clickable:hover { box-shadow: inset 0 0 0 1px var(--up-border-control); }
  .row.selected { box-shadow: inset 0 0 0 1.5px var(--up-accent); }
  .fill {
    position: absolute; left: 0; top: 0; bottom: 0;
    background: var(--up-accent-tint);
    border-radius: var(--up-radius-row, 6px);
    transform-origin: left;
    animation: grow 420ms cubic-bezier(0.2, 0, 0, 1) both;
    transition: width 300ms cubic-bezier(0.2, 0, 0, 1);
  }
  @keyframes grow { from { transform: scaleX(0); } to { transform: scaleX(1); } }
  @media (prefers-reduced-motion: reduce) { .fill { animation: none; } }
  .left { position: relative; display: flex; align-items: center; gap: 8px; min-width: 0; }
  .prefix { font-size: 14px; line-height: 1; }
  .label { font: var(--up-type-meta); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .value { position: relative; font: var(--up-type-meta); font-weight: 600; color: var(--up-text-muted); flex-shrink: 0; }
  .empty { font: var(--up-type-meta); color: var(--up-text-faint); padding: 6px 10px 8px; }
</style>
