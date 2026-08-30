<script lang="ts">
  let { checked = $bindable(false), disabled = false, onchange, label }: { checked?: boolean; disabled?: boolean; onchange?: (v: boolean) => void; label?: string } = $props()
  function toggle() {
    if (disabled) return
    checked = !checked
    onchange?.(checked)
  }
</script>

<button type="button" role="switch" aria-checked={checked} aria-label={label} class:on={checked} {disabled} onclick={toggle}>
  <span class="knob"></span>
</button>

<style>
  button {
    width: 34px;
    height: 20px;
    border-radius: var(--up-radius-pill);
    border: none;
    padding: 0;
    position: relative;
    cursor: pointer;
    background: var(--up-border-control);
    transition: background 120ms ease-out;
    flex-shrink: 0;
  }
  button.on { background: var(--up-accent); }
  button:disabled { opacity: 0.5; cursor: default; }
  .knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: var(--up-bg);
    box-shadow: var(--up-shadow-knob);
    transition: left 120ms ease-out;
  }
  button.on .knob { left: 16px; }
</style>
