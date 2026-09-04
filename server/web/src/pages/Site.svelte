<script lang="ts">
  // Per-site dashboard: metrics, chart, breakdowns, world map, settings.
  import { api, googleConnectURL, polarApi, RANGES, refIconURL, siteIconURL, type Dim, type Filters, type GoogleStatus, type Live, type PolarStatus, type Range, type Revenue, type RevenueDim, type Row, type SearchTerm, type Site, type Summary } from '../lib/api'
  import { countryName, flag, fmtDelta, fmtMoney, fmtNum, fmtRatio } from '../lib/format'
  import { pageIn, panel } from '../lib/motion'
  import Icon from '../lib/ui/Icon.svelte'
  import Segment from '../lib/ui/Segment.svelte'
  import MetricStat from '../lib/ui/MetricStat.svelte'
  import BarList, { type BarRow } from '../lib/ui/BarList.svelte'
  import AreaChart from '../lib/ui/AreaChart.svelte'
  import Input from '../lib/ui/Input.svelte'
  import Button from '../lib/ui/Button.svelte'
  import Modal from '../lib/ui/Modal.svelte'
  import BrandIcon from '../lib/ui/BrandIcon.svelte'
  import Realtime from '../lib/ui/Realtime.svelte'

  let { id }: { id: string } = $props()
  let site = $state<Site | null>(null)
  let stats = $state<Summary | null>(null)
  let live = $state(0)
  let range = $state<Range>('7d')
  let error = $state('')
  // Click-to-filter: dimension to key, mirrored into the URL so back and
  // share work. Filtered views come from raw events, so the server may
  // truncate them to the retention window.
  const DIMS: Dim[] = ['page', 'ref', 'country', 'region', 'device', 'browser', 'os', 'event', 'utm_source', 'utm_campaign']
  function filtersFromURL(): Filters {
    const q = new URLSearchParams(location.search)
    const f: Filters = {}
    for (const d of DIMS) if (q.has(d)) f[d] = q.get(d) ?? ''
    return f
  }
  let filters = $state<Filters>(filtersFromURL())
  const hasFilters = $derived(Object.keys(filters).length > 0)
  function setFilters(next: Filters) {
    filters = next
    const q = new URLSearchParams(location.search)
    for (const d of DIMS) q.delete(d)
    for (const [d, k] of Object.entries(next)) if (k !== undefined) q.set(d, k)
    const s = q.toString()
    history.pushState(null, '', location.pathname + (s ? '?' + s : ''))
  }
  function toggleFilter(dim: Dim, key: string) {
    const next = { ...filters }
    if (next[dim] === key) delete next[dim]
    else next[dim] = key
    setFilters(next)
  }
  const select = (dim: Dim) => (r: BarRow) => toggleFilter(dim, r.key === '∅' ? '' : r.key === 'direct' ? '' : r.key === 'XX' ? '' : r.key)
  $effect(() => {
    const onPop = () => (filters = filtersFromURL())
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  })
  const filterLabel = (dim: Dim, key: string) =>
    dim === 'country' ? countryName(key) || 'Unknown' : dim === 'ref' ? key || 'Direct' : key || 'Unknown'
  const FILTER_DIM: Record<Dim, string> = {
    page: 'Page', ref: 'Referrer', utm_source: 'Source', utm_campaign: 'Campaign', country: 'Country', region: 'Region',
    browser: 'Browser', device: 'Device', os: 'OS', event: 'Event',
  }
  const selectedKey = (dim: Dim) => (filters[dim] === undefined ? undefined : filters[dim] === '' ? (dim === 'ref' ? 'direct' : dim === 'country' ? 'XX' : '∅') : filters[dim])
  let settingsOpen = $state(false)
  let copied = $state(false)
  // The map pulls in MapLibre; load it only once the dashboard is up.
  let WorldMap = $state<typeof import('../lib/map/WorldMap.svelte').default | null>(null)
  let LiveMap = $state<typeof import('../lib/map/LiveMap.svelte').default | null>(null)
  let mapView = $state<'live' | 'range'>('live')
  let liveData = $state<Live | null>(null)

  // Google Search Console: connection status for the settings panel and
  // the search terms it feeds. Google's data trails by two to three days.
  let google = $state<GoogleStatus | null>(null)
  let googleRedirect = $state('')
  let googleBusy = $state(false)
  let googleNotice = $state('')
  let terms = $state<SearchTerm[]>([])

  async function loadGoogle() {
    try {
      const r = await api.google(id)
      google = r.status
      googleRedirect = r.redirect_uri
    } catch (e: any) {
      google = null
    }
  }
  async function loadTerms() {
    if (!google?.connected) {
      terms = []
      return
    }
    try {
      terms = (await api.searchTerms(id, range)).rows
    } catch {
      terms = []
    }
  }
  async function googleAction(run: () => Promise<unknown>) {
    googleBusy = true
    try {
      await run()
      await loadGoogle()
      await loadTerms()
    } catch (e: any) {
      error = e.message
    } finally {
      googleBusy = false
    }
  }
  function disconnectGoogle() {
    if (!confirm('Disconnect Google Search Console? Stored search terms for this site are deleted.')) return
    googleAction(() => api.googleDisconnect(id))
  }
  // The callback lands here with ?google=connected or ?google_error=…
  $effect(() => {
    const params = new URLSearchParams(location.search)
    const err = params.get('google_error')
    if (params.get('google') === 'connected') {
      googleNotice = 'Google Search Console connected. The first pull can take a minute.'
      settingsOpen = true
    } else if (err) {
      error = 'Google: ' + err
      settingsOpen = true
    }
    if (params.has('google') || params.has('google_error')) history.replaceState(null, '', location.pathname)
    loadGoogle()
  })
  $effect(() => {
    range
    google?.connected
    loadTerms()
  })
  // The card shows the same top 10 as the other breakdowns; the modal has everything.
  const TOP_TERMS = 10
  // Polar: revenue next to traffic, attributed to first touch when the
  // site passes it into checkout metadata.
  let polar = $state<PolarStatus | null>(null)
  let revenue = $state<Revenue | null>(null)
  let polarBusy = $state(false)
  let polarForm = $state({ access_token: '', server: '', product_ids: '', webhook_secret: '' })
  let polarOpen = $state(false)
  let revenueTab = $state<RevenueDim>('ref')

  async function loadPolar() {
    try {
      polar = await polarApi.status(id)
      if (polar.connection) polarForm = { access_token: '', server: polar.connection.server, product_ids: polar.connection.product_ids, webhook_secret: '' }
    } catch {
      polar = null
    }
  }
  async function loadRevenue() {
    if (!polar?.connected) {
      revenue = null
      return
    }
    try {
      revenue = await polarApi.revenue(id, range)
    } catch {
      revenue = null
    }
  }
  async function polarAction(run: () => Promise<unknown>) {
    polarBusy = true
    try {
      await run()
      error = ''
      polarOpen = false
      await loadPolar()
      await loadRevenue()
    } catch (e: any) {
      error = e.message
    } finally {
      polarBusy = false
    }
  }
  function savePolar() {
    const input: Parameters<typeof polarApi.connect>[1] = { server: polarForm.server, product_ids: polarForm.product_ids }
    if (polarForm.access_token.trim()) input.access_token = polarForm.access_token.trim()
    if (polarForm.webhook_secret.trim()) input.webhook_secret = polarForm.webhook_secret.trim()
    polarAction(() => polarApi.connect(id, input))
  }
  function disconnectPolar() {
    if (!confirm('Disconnect Polar? Stored orders for this site are deleted.')) return
    polarAction(() => polarApi.disconnect(id))
  }
  $effect(() => {
    loadPolar()
  })
  $effect(() => {
    range
    polar?.connected
    loadRevenue()
  })
  const REVENUE_DIMS: { value: RevenueDim; label: string }[] = [
    { value: 'ref', label: 'Referrer' }, { value: 'source', label: 'Source' }, { value: 'campaign', label: 'Campaign' },
    { value: 'landing', label: 'Landing' }, { value: 'country', label: 'Country' }, { value: 'product', label: 'Product' },
  ]
  const revenueRows = $derived(
    (revenue?.breakdowns[revenueTab] ?? []).map((r) => ({
      key: r.key || '∅',
      label: revenueTab === 'country' ? countryName(r.key) || 'Unknown' : revenueTab === 'ref' ? r.key || 'Direct or unattributed' : r.key || 'Unattributed',
      value: r.revenue,
      prefix: revenueTab === 'country' ? flag(r.key) : undefined,
      icon: revenueTab === 'ref' && r.key ? refIconURL(r.key) : '',
      direct: revenueTab === 'ref' && r.key === '',
      title: `${r.orders} ${r.orders === 1 ? 'order' : 'orders'}`,
    })),
  )
  const money = $derived((v: number) => fmtMoney(v, revenue?.currency ?? ''))
  const revenueSeries = $derived(revenue && stats && revenue.series.length === stats.series.length ? revenue.series.map((p) => p.revenue) : [])

  const termRows = $derived(
    terms.map((t) => ({
      key: t.query,
      label: t.query,
      value: t.clicks,
      title: `${fmtNum(t.impressions)} impressions · position ${t.position.toFixed(1)}`,
    })),
  )

  async function load() {
    try {
      const r = await api.stats(id, range, filters)
      site = r.site
      stats = r.stats
      live = r.live
      error = ''
      document.title = `${r.site.name} · Glance`
    } catch (e: any) {
      error = e.message
    }
  }
  $effect(() => {
    range
    filters
    load()
  })
  $effect(() => {
    const t = setInterval(load, 60_000)
    import('../lib/map/WorldMap.svelte').then((m) => (WorldMap = m.default)).catch(() => {})
    import('../lib/map/LiveMap.svelte').then((m) => (LiveMap = m.default)).catch(() => {})
    return () => clearInterval(t)
  })
  // Live snapshot every 5s: feeds the realtime card and the live globe.
  $effect(() => {
    const poll = () => api.live(id).then((l) => (liveData = l)).catch(() => {})
    poll()
    const t = setInterval(poll, 5000)
    return () => clearInterval(t)
  })

  // One mapping per dimension so the cards and the "view all" modal agree.
  const toRows = (dim: Dim, src: Row[]): BarRow[] => {
    const sorted = [...src].sort((a, b) => (dim === 'event' ? b.pageviews - a.pageviews : b.visitors - a.visitors))
    switch (dim) {
      case 'ref':
        return sorted.map((r) => ({ key: r.key || 'direct', label: r.key || 'Direct', value: r.visitors, direct: r.key === '', icon: r.key ? refIconURL(r.key) : '', title: `${fmtNum(r.pageviews)} views` }))
      case 'country':
        return sorted.map((r) => ({ key: r.key || 'XX', label: countryName(r.key), value: r.visitors, prefix: flag(r.key), title: `${fmtNum(r.pageviews)} views` }))
      case 'event':
        return sorted.map((r) => ({ key: r.key, label: r.key, value: r.pageviews, title: `${fmtNum(r.visitors)} visitors` }))
      default:
        return sorted.map((r) => ({ key: r.key || '∅', label: r.key || 'Unknown', value: r.visitors, title: `${fmtNum(r.pageviews)} views` }))
    }
  }
  const DIM_TITLE: Record<Dim, string> = {
    page: 'Pages', ref: 'Referrers', utm_source: 'Sources', utm_campaign: 'Campaigns', country: 'Countries', region: 'Regions',
    browser: 'Browsers', device: 'Devices', os: 'Operating systems', event: 'Events',
  }
  // Card tabs, as in the reference: one card per group, a dimension per tab.
  let sourceTab = $state<'ref' | 'utm_source' | 'utm_campaign'>('ref')
  let locationTab = $state<'country' | 'region'>('country')
  let deviceTab = $state<'browser' | 'os' | 'device'>('browser')
  const rowsFor = (dim: Dim) => toRows(dim, stats?.breakdowns[dim] ?? [])
  const pages = $derived(rowsFor('page'))
  const sources = $derived(rowsFor(sourceTab))
  const locations = $derived(rowsFor(locationTab))
  const devices = $derived(rowsFor(deviceTab))
  const events = $derived(rowsFor('event'))

  // "View all" modal.
  let modal = $state<Dim | 'search' | null>(null)
  let modalRows = $state<BarRow[] | null>(null)
  let filter = $state('')
  function openAll(dim: Dim | 'search') {
    modal = dim
    modalRows = null
    filter = ''
    if (dim === 'search') {
      modalRows = termRows
      return
    }
    api
      .breakdown(id, dim, range, filters)
      .then((r) => (modalRows = toRows(dim, r.rows)))
      .catch((e: any) => (error = e.message))
  }
  const filtered = $derived((modalRows ?? []).filter((r) => !filter || r.label.toLowerCase().includes(filter.toLowerCase())))
  const more = (dim: Dim | 'search') => () => openAll(dim)
  const topCountry = $derived(stats?.breakdowns.country?.[0]?.key ?? '')

  const snippet = $derived(site ? `<script defer src="${location.origin}/glance.js" data-site="${site.id}"><\/script>` : '')
  async function copy() {
    try {
      await navigator.clipboard.writeText(snippet)
      copied = true
      setTimeout(() => (copied = false), 1500)
    } catch {}
  }
  let timers: Record<string, ReturnType<typeof setTimeout>> = {}
  function save(patch: Parameters<typeof api.updateSite>[1], key: string) {
    clearTimeout(timers[key])
    timers[key] = setTimeout(() => {
      api
        .updateSite(id, patch)
        .then((s) => (site = { ...site!, ...s }))
        .catch((e: any) => (error = e.message))
    }, 400)
  }
</script>

{#if site && stats}
  <div class="title">
    <Icon src={site.has_favicon ? siteIconURL(site.id) : ''} size={22} />
    <span class="name">{site.name}</span>
    <span class="domain">{site.domain}</span>
    {#if live > 0}<span class="live"><span class="dot"></span>{live} online</span>{/if}
    <span class="spacer"></span>
    <div class="ranges"><Segment options={RANGES.map((r) => ({ value: r, label: r }))} value={range} gap={14} onchange={(r) => (range = r)} /></div>
    <button type="button" class="plain" class:on={settingsOpen} onclick={() => (settingsOpen = !settingsOpen)}>Settings</button>
  </div>

  {#if settingsOpen}
    <div class="settings" transition:panel>
      <div class="setting">
        <div class="text"><div class="label">Tracking code</div><div class="hint">Paste before the closing &lt;/head&gt; tag. No cookies, no consent banner needed.</div></div>
      </div>
      <div class="code">
        <span class="snippet">{snippet}</span>
        <button type="button" class="copy" onclick={copy}>{copied ? 'Copied' : 'Copy'}</button>
      </div>
      <div class="setting">
        <div class="text"><div class="label">Name</div></div>
        <div class="ctl"><Input value={site.name} aria-label="Name" oninput={(e) => save({ name: e.currentTarget.value }, 'name')} /></div>
      </div>
      <div class="setting">
        <div class="text"><div class="label">Domain</div><div class="hint">Events from other hosts are ignored</div></div>
        <div class="ctl"><Input value={site.domain} aria-label="Domain" oninput={(e) => save({ domain: e.currentTarget.value }, 'domain')} /></div>
      </div>
      <div class="setting">
        <div class="text"><div class="label">Home country</div><div class="hint">Where the map arcs converge. Blank uses your top country ({countryName(topCountry) || 'none yet'}).</div></div>
        <div class="ctl short"><Input value={site.home_country} placeholder={topCountry || 'GB'} maxlength={2} aria-label="Home country" oninput={(e) => save({ home_country: e.currentTarget.value.toUpperCase() }, 'home')} /></div>
      </div>
      <div class="setting">
        <div class="text"><div class="label">Favicon</div><div class="hint">Fetched from your site by Glance, never from a third party</div></div>
        <Button variant="secondary" size="sm" onclick={() => api.refreshFavicon(id).then((s) => (site = { ...site!, ...s }))}>Refresh</Button>
      </div>
      {#if polar}
        <div class="setting google">
          <div class="text">
            <div class="label">Polar</div>
            {#if polar.connected && polar.connection}
              <div class="hint">
                {polar.connection.server.replace('https://', '')}{#if polar.connection.product_ids} · {polar.connection.product_ids.split(',').length} {polar.connection.product_ids.includes(',') ? 'products' : 'product'}{/if}
                · {fmtNum(polar.orders)} orders
                {#if polar.connection.sync_error}
                  · <span class="bad">{polar.connection.sync_error}</span>
                {:else if polar.connection.synced_at}
                  · synced {new Date(polar.connection.synced_at).toLocaleString()}
                {:else}
                  · first pull pending
                {/if}
                {#if !polar.connection.has_webhook_secret}
                  · <span class="warn">no webhook, sales appear daily</span>
                {/if}
              </div>
            {:else}
              <div class="hint">Show revenue next to traffic. Needs an organization access token from Polar (Settings, Developers) with the orders:read scope.</div>
            {/if}
            {#if polarOpen}
              <div class="polar-form" transition:panel>
                <Input bind:value={polarForm.access_token} placeholder={polar.connected ? 'Access token (leave blank to keep)' : 'polar_oat_…'} aria-label="Polar access token" type="password" mono />
                <Input bind:value={polarForm.product_ids} placeholder="Product ids, comma separated (blank = all)" aria-label="Polar product ids" mono />
                <Input bind:value={polarForm.webhook_secret} placeholder={polar.connection?.has_webhook_secret ? 'Webhook secret (leave blank to keep)' : 'Webhook secret (optional)'} aria-label="Polar webhook secret" type="password" mono />
                <Input bind:value={polarForm.server} placeholder="https://api.polar.sh" aria-label="Polar API server" mono />
                <div class="hint">Webhook URL for Polar, subscribe to the order events: <code>{polar.webhook_url}</code></div>
                <div class="google-actions">
                  <Button size="sm" disabled={polarBusy} onclick={savePolar}>{polarBusy ? 'Checking' : polar.connected ? 'Save' : 'Connect'}</Button>
                  <Button variant="secondary" size="sm" onclick={() => (polarOpen = false)}>Cancel</Button>
                </div>
              </div>
            {/if}
          </div>
          <div class="google-actions">
            {#if polar.connected}
              <Button variant="secondary" size="sm" disabled={polarBusy} onclick={() => polarAction(() => polarApi.sync(id))}>{polarBusy ? 'Working' : 'Sync now'}</Button>
              <Button variant="secondary" size="sm" onclick={() => (polarOpen = !polarOpen)}>Edit</Button>
              <Button variant="secondary" size="sm" disabled={polarBusy} onclick={disconnectPolar}>Disconnect</Button>
            {:else}
              <Button variant="secondary" size="sm" onclick={() => (polarOpen = !polarOpen)}>Connect Polar</Button>
            {/if}
          </div>
        </div>
      {/if}
      {#if google}
        <div class="setting google">
          <div class="text">
            <div class="label">Google Search Console</div>
            {#if !google.configured}
              <div class="hint">Shows the search terms Google sends here. Create an OAuth client in Google Cloud, add <code>{googleRedirect}</code> as a redirect URI, then set <code>GLANCE_GOOGLE_CLIENT_ID</code> and <code>GLANCE_GOOGLE_CLIENT_SECRET</code>.</div>
            {:else if google.connected && google.connection}
              <div class="hint">
                {google.connection.email || 'Connected'}{#if google.connection.property} · {google.connection.property}{/if}
                {#if google.needs_reconnect}
                  · <span class="bad">access expired, connect again</span>
                {:else if google.connection.sync_error}
                  · <span class="bad">{google.connection.sync_error}</span>
                {:else if google.latest_day}
                  · data to {google.latest_day}
                {:else if google.connection.property}
                  · first pull pending
                {/if}
              </div>
              {#if !google.connection.property && google.available_properties?.length}
                <div class="hint">No property matches {site.domain}. Pick one:</div>
                <div class="props">
                  {#each google.available_properties as p (p)}
                    <button type="button" class="prop" disabled={googleBusy} onclick={() => googleAction(() => api.googleSetProperty(id, p))}>{p}</button>
                  {/each}
                </div>
              {:else if !google.connection.property}
                <div class="hint bad">This Google account has no Search Console property for {site.domain}. Verify the site in Search Console, then connect again.</div>
              {/if}
            {:else}
              <div class="hint">Shows the search terms Google sends here. Read-only access; Glance pulls once a day.</div>
            {/if}
            {#if googleNotice}<div class="hint ok">{googleNotice}</div>{/if}
          </div>
          <div class="google-actions">
            {#if google.connected && !google.needs_reconnect}
              <Button variant="secondary" size="sm" disabled={googleBusy || !google.connection?.property} onclick={() => googleAction(() => api.googleSync(id))}>{googleBusy ? 'Working' : 'Sync now'}</Button>
              <Button variant="secondary" size="sm" disabled={googleBusy} onclick={disconnectGoogle}>Disconnect</Button>
            {:else if google.configured}
              <a class="btn" href={googleConnectURL(id)}>{google.needs_reconnect ? 'Reconnect Google' : 'Connect Google Search Console'}</a>
            {/if}
          </div>
        </div>
      {/if}
    </div>
  {/if}

  <div class="strip">
    <div class="metrics">
      <MetricStat label="Visitors" value={fmtNum(stats.totals.visitors)} delta={stats.previous_unavailable ? '' : fmtDelta(stats.totals.visitors, stats.previous.visitors)} />
      <MetricStat label="Page views" value={fmtNum(stats.totals.pageviews)} delta={stats.previous_unavailable ? '' : fmtDelta(stats.totals.pageviews, stats.previous.pageviews)} />
      <MetricStat label="Views / visitor" value={fmtRatio(stats.totals.pageviews, stats.totals.visitors)} />
      {#if revenue && !hasFilters}
        <div class="group">
          <MetricStat label="Revenue" value={money(revenue.totals.revenue)} delta={fmtDelta(revenue.totals.revenue, revenue.previous.revenue)} />
          <MetricStat label="Orders" value={fmtNum(revenue.totals.orders)} delta={fmtDelta(revenue.totals.orders, revenue.previous.orders)} />
          <MetricStat label="Per visitor" value={stats.totals.visitors ? money(Math.round(revenue.totals.revenue / stats.totals.visitors)) : money(0)} />
        </div>
      {/if}
    </div>
  </div>

  {#if hasFilters}
    <div class="filters" transition:panel>
      <span class="filters-label">Showing visitors who match</span>
      {#each Object.entries(filters) as [dim, key] (dim)}
        <button type="button" class="chip" onclick={() => toggleFilter(dim as Dim, key ?? '')} title="Remove filter">
          <span class="chip-dim">{FILTER_DIM[dim as Dim]}</span>{filterLabel(dim as Dim, key ?? '')}<span class="chip-x">×</span>
        </button>
      {/each}
      <button type="button" class="plain" onclick={() => setFilters({})}>Clear</button>
      <span class="filters-note">
        {#if stats.truncated}Filters read raw events, kept {stats.retention_days} days, so this range is cut short. Raise retention in Settings to filter further back.{:else}Realtime, revenue and search terms cannot be filtered; revenue and search terms are hidden.{/if}
      </span>
    </div>
  {/if}

  {#key stats.range}
    <div in:pageIn class="stack">
      <AreaChart series={stats.series} markers={stats.markers} bucket={stats.bucket} {range} revenue={hasFilters ? [] : revenueSeries} currency={revenue?.currency ?? ''} />

      <div class="grid">
        {#if revenue && !hasFilters}
          <div class="wide">
          <BarList
            title="Revenue"
            rows={revenueRows}
            empty={revenueTab === 'ref' || revenueTab === 'source' || revenueTab === 'campaign' || revenueTab === 'landing' ? 'No attributed orders in this range. Pass attribution into checkout to see where sales come from.' : 'No orders in this range'}
            tabs={REVENUE_DIMS}
            tab={revenueTab}
            ontab={(v) => (revenueTab = v)}
            format={money}
          >
            {#snippet icon(r)}{#if revenueTab === 'ref'}<Icon src={r.icon} direct={r.direct} />{/if}{/snippet}
          </BarList>
          </div>
        {/if}
        <Realtime minutes={liveData?.minutes ?? Array(30).fill(0)} total={liveData?.total_30m ?? 0} onmore={() => { mapView = 'live'; document.querySelector('.map-section')?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }} />
        {#if google?.connected && !hasFilters}
          <BarList title="Search terms" rows={termRows.slice(0, TOP_TERMS)} empty={range === '24h' ? 'Google reports search terms two to three days late' : 'No Google search terms for this range yet'} onmore={more('search')} />
        {/if}
        <BarList title="Pages" rows={pages} empty="No page views yet" onmore={more('page')} onselect={select('page')} selected={selectedKey('page')} />
        <BarList
          title="Sources"
          rows={sources}
          empty={sourceTab === 'ref' ? 'No referrers yet' : sourceTab === 'utm_source' ? 'No utm_source or ?ref= tags seen' : 'No utm_campaign tags seen'}
          onmore={more(sourceTab)}
          onselect={select(sourceTab)}
          selected={selectedKey(sourceTab)}
          tabs={[{ value: 'ref', label: 'Referrer' }, { value: 'utm_source', label: 'Source' }, { value: 'utm_campaign', label: 'Campaign' }]}
          tab={sourceTab}
          ontab={(v) => (sourceTab = v)}
        >
          {#snippet icon(r)}{#if sourceTab === 'ref'}<Icon src={r.icon} direct={r.direct} />{/if}{/snippet}
        </BarList>
        <BarList
          title="Locations"
          rows={locations}
          empty={locationTab === 'country' ? 'No locations yet' : 'No regions yet'}
          onmore={more(locationTab)}
          onselect={select(locationTab)}
          selected={selectedKey(locationTab)}
          tabs={[{ value: 'country', label: 'Countries' }, { value: 'region', label: 'Regions' }]}
          tab={locationTab}
          ontab={(v) => (locationTab = v)}
        />
        <BarList
          title="Devices"
          rows={devices}
          onmore={more(deviceTab)}
          onselect={select(deviceTab)}
          selected={selectedKey(deviceTab)}
          tabs={[{ value: 'browser', label: 'Browsers' }, { value: 'os', label: 'OS' }, { value: 'device', label: 'Devices' }]}
          tab={deviceTab}
          ontab={(v) => (deviceTab = v)}
        >
          {#snippet icon(r)}<BrandIcon kind={deviceTab} name={r.key} />{/snippet}
        </BarList>
        {#if events.length > 0}
          <BarList title="Events" rows={events} onmore={more('event')} onselect={select('event')} selected={selectedKey('event')} />
        {/if}
      </div>

      <div class="map-section">
        <div class="head">
          <div class="card-title">{mapView === 'live' ? 'Live' : 'Visitors by country'}</div>
          <div class="map-controls">
            <span class="hint">{mapView === 'live' ? `${liveData?.total ?? live} online` : `${stats.breakdowns.country.length} ${stats.breakdowns.country.length === 1 ? 'country' : 'countries'}`}</span>
            <Segment options={[{ value: 'live', label: 'Live' }, { value: 'range', label: range }]} value={mapView} gap={14} onchange={(v) => (mapView = v)} />
          </div>
        </div>
        {#if mapView === 'live'}
          {#if LiveMap && liveData}
            <LiveMap live={liveData} home={site.home_country || topCountry} />
          {:else}
            <div class="map-placeholder tall"></div>
          {/if}
        {:else if WorldMap}
          <WorldMap rows={stats.breakdowns.country} home={site.home_country || topCountry} />
        {:else}
          <div class="map-placeholder"></div>
        {/if}
      </div>
    </div>
  {/key}
{:else if error}
  <p class="bad">{error}</p>
{/if}

{#if modal}
  <Modal title={modal === 'search' ? 'Search terms' : DIM_TITLE[modal]} subtitle="{site?.name} · {range} · {modalRows ? `${modalRows.length} ${modal === 'event' ? 'events' : modal === 'search' ? 'terms' : 'rows'}` : 'loading'}" onclose={() => (modal = null)}>
    <div class="modal-search"><Input bind:value={filter} placeholder="Filter" aria-label="Filter rows" /></div>
    {#if modalRows}
      <BarList bare rows={filtered} empty="No matches">
        {#snippet icon(r)}
          {#if modal === 'ref'}<Icon src={r.icon} direct={r.direct} />
          {:else if modal === 'browser' || modal === 'os' || modal === 'device'}<BrandIcon kind={modal} name={r.key} />{/if}
        {/snippet}
      </BarList>
    {/if}
  </Modal>
{/if}

<style>
  .modal-search { position: sticky; top: 0; background: var(--up-bg); padding: 4px 0 12px; z-index: 1; }
  .title { display: flex; align-items: center; gap: 10px; margin-top: 4px; }
  .title .ranges { margin-right: 22px; }
  .name { font: var(--up-type-row-title); }
  .domain { font: var(--up-type-meta); color: var(--up-text-muted); }
  .live { display: flex; align-items: center; gap: 6px; font: var(--up-type-meta); color: var(--up-text-muted); margin-left: 6px; }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--up-accent); }
  .spacer { flex: 1; }
  .plain { background: none; border: none; padding: 0; cursor: pointer; font: var(--up-type-ui); color: var(--up-text-muted); }
  .plain:hover, .plain.on { color: var(--up-ink); }

  .settings { display: flex; flex-direction: column; gap: 14px; padding: 16px 18px; border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); margin-top: -16px; }
  .setting { display: flex; align-items: center; justify-content: space-between; gap: 24px; }
  .text { display: flex; flex-direction: column; gap: 2px; }
  .label { font: var(--up-type-setting); }
  .hint { font: var(--up-type-meta); color: var(--up-text-muted); }
  .ctl { width: 240px; flex-shrink: 0; }
  .ctl.short { width: 90px; }
  .code { background: var(--up-surface-dark); border-radius: var(--up-radius-tooltip); padding: 12px 14px; display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
  .snippet { font: var(--up-type-code); color: var(--up-text-on-dark); word-break: break-all; }
  .copy { background: none; border: none; padding: 2px 0; cursor: pointer; font: var(--up-type-small); color: var(--up-operational-strong); flex-shrink: 0; }
  .copy:hover { color: var(--up-text-on-dark); }
  .google { align-items: flex-start; }
  .google .text { gap: 6px; }
  .hint code { font: var(--up-type-code); user-select: all; }
  .hint.ok { color: var(--up-operational-strong); }
  .warn { color: var(--up-text-muted); }
  .polar-form { display: flex; flex-direction: column; gap: 8px; width: 100%; max-width: 460px; padding-top: 4px; }
  .google-actions { display: flex; gap: 8px; flex-shrink: 0; }
  .btn { font: var(--up-type-ui); color: var(--up-ink); background: var(--up-bg); box-shadow: inset 0 0 0 1px var(--up-border-control); border-radius: var(--up-radius-control); height: 30px; padding: 0 12px; display: inline-flex; align-items: center; white-space: nowrap; }
  .btn:hover { background: var(--up-bg-hover); color: var(--up-ink); }
  .props { display: flex; flex-wrap: wrap; gap: 6px; }
  .prop { font: var(--up-type-code); color: var(--up-ink); background: var(--up-bg-hover); border: none; border-radius: var(--up-radius-control); padding: 4px 8px; cursor: pointer; }
  .prop:hover { box-shadow: inset 0 0 0 1px var(--up-border-control); }
  .prop:disabled { opacity: 0.5; cursor: default; }

  .filters { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: -12px; }
  .filters-label { font: var(--up-type-meta); color: var(--up-text-muted); }
  .chip { display: inline-flex; align-items: center; gap: 6px; font: var(--up-type-meta); color: var(--up-ink); background: var(--up-accent-tint); border: none; border-radius: var(--up-radius-pill); padding: 4px 8px 4px 10px; cursor: pointer; }
  .chip:hover { box-shadow: inset 0 0 0 1px var(--up-accent); }
  .chip-dim { color: var(--up-text-muted); }
  .chip-x { color: var(--up-text-muted); font-size: 14px; line-height: 1; }
  .filters .plain { margin-left: 4px; }
  .filters-note { font: var(--up-type-meta); color: var(--up-text-muted); width: 100%; }
  .strip { display: flex; align-items: flex-start; }
  .metrics { display: flex; gap: 28px; flex-wrap: nowrap; }
  .group { display: flex; gap: 28px; padding-left: 28px; border-left: 1px solid var(--up-border-hairline); }
  .stack { display: flex; flex-direction: column; gap: 36px; min-width: 0; }
  .stack > :global(*) { min-width: 0; }
  /* minmax(0, 1fr): a plain 1fr column grows to fit its widest child, which is how cards escaped the viewport on phones. */
  .grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 20px; }
  .grid > :global(*) { min-width: 0; }
  .wide { grid-column: 1 / -1; }
  .map-section { display: flex; flex-direction: column; gap: 12px; }
  .head { display: flex; align-items: baseline; justify-content: space-between; }
  .card-title { font: var(--up-type-setting); font-weight: 700; }
  .map-placeholder { height: 340px; border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); }
  .map-placeholder.tall { height: 420px; }
  .map-controls { display: flex; align-items: center; gap: 18px; }
  .bad { font: var(--up-type-meta); color: var(--up-degraded-strong); }
  @media (max-width: 600px) {
    .grid { grid-template-columns: minmax(0, 1fr); }
    .title { flex-wrap: wrap; row-gap: 8px; }
    .strip { min-width: 0; overflow: hidden; }
    .title .ranges { margin-right: 0; width: 100%; order: 9; }
    .metrics { gap: 24px; flex-wrap: wrap; row-gap: 16px; }
    .group { gap: 24px; padding-left: 0; border-left: none; width: 100%; }
    .setting { flex-direction: column; align-items: flex-start; gap: 8px; }
    .ctl { width: 100%; }
  }
</style>
