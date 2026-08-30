<script lang="ts">
  import type { Snippet } from 'svelte'
  import type { HTMLButtonAttributes } from 'svelte/elements'

  interface Props extends HTMLButtonAttributes {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
    size?: 'md' | 'sm'
    children: Snippet
  }
  let { variant = 'primary', size = 'md', children, ...rest }: Props = $props()
</script>

<button class="btn {variant} {size}" {...rest}>{@render children()}</button>

<style>
  .btn {
    font: var(--up-type-ui);
    border: none;
    cursor: pointer;
    border-radius: var(--up-radius-control);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--up-space-2);
    height: 34px;
    padding: 0 16px;
    white-space: nowrap;
    transition: background 120ms ease-out, color 120ms ease-out;
  }
  .sm { height: 28px; padding: 0 12px; }
  .btn:disabled { cursor: default; }

  .primary { background: var(--up-accent); color: var(--up-text-on-dark); }
  .primary:hover:not(:disabled) { background: var(--up-accent-hover); }
  .primary:disabled { background: var(--up-text-inactive); }

  .secondary { background: var(--up-bg); color: var(--up-ink); box-shadow: inset 0 0 0 1px var(--up-border-control); }
  .secondary:hover:not(:disabled) { background: var(--up-bg-hover); }
  .secondary:disabled { color: var(--up-text-inactive); }

  .ghost { background: transparent; color: var(--up-accent); }
  .ghost:hover:not(:disabled) { background: var(--up-accent-tint); color: var(--up-accent-hover); }
  .ghost:disabled { color: var(--up-text-inactive); }

  .danger { background: transparent; color: var(--up-text-secondary); box-shadow: inset 0 0 0 1px var(--up-border-control); }
  .danger:hover:not(:disabled) { color: var(--up-outage-strong); background: var(--up-bg-hover); }
</style>
