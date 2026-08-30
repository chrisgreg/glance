<script lang="ts">
  // The analytics hero chart: LayerChart primitives, smoothed periwinkle line
  // over a fading fill, with the design's own dark tooltip.
  import { Chart, Svg, Area, Highlight, LinearGradient, Tooltip } from 'layerchart'
  import { curveMonotoneX } from 'd3-shape'
  import type { Point } from '../api'
  import { fmtNum, fmtPoint } from '../format'

  let { series, bucket, range }: { series: Point[]; bucket: 'hour' | 'day'; range: string } = $props()
  const data = $derived(series.map((p) => ({ ...p, date: new Date(p.t) })))
  const max = $derived(Math.max(1, ...series.map((p) => p.visitors)))
  const first = $derived(series[0]?.t)
  const last = $derived(series[series.length - 1]?.t)
</script>

<div class="chart">
  <div class="plot">
    <Chart {data} x="date" y="visitors" yDomain={[0, max * 1.15]} padding={{ top: 8, bottom: 0, left: 0, right: 0 }} tooltipContext={{ mode: 'bisect-x' }}>
      <Svg>
        <LinearGradient class="fill" stops={['var(--up-accent)', 'transparent']} vertical>
          {#snippet children({ gradient })}
            <Area y1="visitors" curve={curveMonotoneX} fill={gradient} fillOpacity={0.18} line={{ stroke: 'var(--up-accent-line)', strokeWidth: 2 }} />
          {/snippet}
        </LinearGradient>
        <Highlight lines={{ stroke: 'var(--up-border-control)', strokeWidth: 1 }} points={{ r: 3.5, fill: 'var(--up-accent)', stroke: 'var(--up-bg)', strokeWidth: 2 }} />
      </Svg>
      <Tooltip.Root variant="none" y="data" xOffset={0} yOffset={12} anchor="bottom">
        {#snippet children({ data })}
          <div class="tip">
            <div class="label">{fmtPoint(data.t, bucket, range)}</div>
            <div class="divider"></div>
            <div class="rows">
              <div class="row"><span>Visitors</span><b>{fmtNum(data.visitors)}</b></div>
              <div class="row"><span>Page views</span><b>{fmtNum(data.pageviews)}</b></div>
            </div>
          </div>
        {/snippet}
      </Tooltip.Root>
    </Chart>
  </div>
  <div class="axis">
    <span>{first ? fmtPoint(first, bucket, range) : ''}</span>
    <span>{range === '24h' ? 'Now' : 'Today'}</span>
  </div>
</div>

<style>
  .chart { display: flex; flex-direction: column; gap: 8px; }
  .plot { height: 200px; width: 100%; }
  .axis { display: flex; justify-content: space-between; font: var(--up-type-caption); color: var(--up-text-faint); }
  .tip {
    background: var(--up-surface-dark);
    color: var(--up-text-on-dark);
    border: 1px solid var(--up-divider-on-dark);
    border-radius: var(--up-radius-tooltip);
    padding: 12px 14px;
    width: 170px;
    box-shadow: var(--up-shadow-tooltip);
    pointer-events: none;
  }
  .label { font: var(--up-type-small); font-weight: 700; }
  .divider { height: 1px; background: var(--up-divider-on-dark); margin: 9px 0; }
  .rows { display: flex; flex-direction: column; gap: 6px; }
  .row { display: flex; justify-content: space-between; font: var(--up-type-small); }
  .row span { color: var(--up-text-on-dark-muted); font-weight: 500; }
</style>
