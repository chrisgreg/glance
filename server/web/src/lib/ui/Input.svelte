<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements'
  interface Props extends HTMLInputAttributes {
    value?: string
    mono?: boolean
  }
  let { value = $bindable(''), mono = false, ...rest }: Props = $props()
</script>

<input class:mono bind:value {...rest} />

<style>
  input {
    font: var(--up-type-meta);
    color: var(--up-ink);
    background: var(--up-bg);
    height: 34px;
    padding: 0 12px;
    border-radius: var(--up-radius-control);
    border: 1px solid var(--up-border-control);
    outline: none;
    width: 100%;
    transition: border-color 120ms ease-out;
  }
  input.mono { font: var(--up-type-code); }
  input:focus { border-color: var(--up-accent-line); }
  input::placeholder { color: var(--up-text-inactive); }
</style>
