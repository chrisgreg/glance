<p align="center">
  <img src="docs/glance.svg" width="160" alt="Glance logo" />

  <h1 align="center">Glance</h1>

<p align="center">
  <img src="https://img.shields.io/github/go-mod/go-version/chrisgreg/glance?filename=server%2Fgo.mod" alt="Go version" />
  <img src="https://img.shields.io/github/license/chrisgreg/glance" alt="License" />
  <img src="https://github.com/chrisgreg/glance/actions/workflows/ci.yml/badge.svg" alt="CI" />
</p>

A tiny, self-hosted web analytics service. The useful stuff at a glance: visitors, page views, top pages, referrers, countries, devices and simple custom events. Not a product analytics platform.

One Go binary, one SQLite file, one Docker container. Sites add a cookieless snippet of about 1 KB. No sessions, funnels, cohorts, heatmaps or replay. No third-party services at runtime: favicons are fetched by Glance itself, the world map ships its own outlines, and brand icons are bundled.

</p>

```html
<script defer src="https://glance.example.com/glance.js" data-site="site_…"></script>
```

## Architecture

The snippet posts one small JSON body per page view (site id, URL, referrer, screen width, time zone) with `sendBeacon`, and again on `pushState` and `popstate` for single-page apps. The Go server validates the host against the site's domain, drops bots, derives browser, OS, device, country, region and campaign tags, and pushes the event onto an in-memory queue. The request never touches the database. A writer goroutine commits the queue every second or every 200 events in one transaction. Every minute (and on demand when the dashboard is opened) today's and yesterday's rollups are rebuilt from raw events; the dashboard reads only rollups, so it stays fast however much traffic you keep. The embedded Svelte UI lists your sites and, per site, shows a chart, tabbed breakdown cards, a live 3D globe and a range map.

## What is in the box

| Part | Where |
| --- | --- |
| Go server (collect endpoint, batched writer, rollups, favicons, MCP, embedded web UI) | `server/` |
| Web UI (Svelte 5, LayerChart, MapLibre, built into the binary) | `server/web/` |
| Tracking snippet (served at `/glance.js`) | `server/internal/api/glance.js` |

## Quick start (Docker)

```bash
git clone https://github.com/chrisgreg/glance && cd glance
cp .env.example .env          # set GLANCE_ADMIN_USER and GLANCE_ADMIN_PASSWORD
mkdir -p data && chown 1000:1000 data   # Linux hosts only; the container runs as uid 1000
docker compose up -d --build   # or drop --build to pull ghcr.io/chrisgreg/glance
open http://localhost:8082
```

Prebuilt images are published to `ghcr.io/chrisgreg/glance` on every release: `latest`, `1`, `1.0`, `1.0.0` and so on for linux/amd64 and linux/arm64.

Add a website, click **Tracking code**, and paste the snippet before `</head>`. Data appears as soon as you open the dashboard.

Data lives in `./data/glance.db`. Back up by copying that file (use `sqlite3 data/glance.db ".backup backup.db"` for a consistent copy while running).

## Quick start (binary)

```bash
make web                       # builds the Svelte UI into the Go embed directory
make build                     # bin/glance with the UI embedded
GLANCE_DATABASE_PATH=./glance.db ./bin/glance     # listens on :8080
```

Configuration is the same set of environment variables as Docker (see [Configuration](#configuration)). `GLANCE_DATABASE_PATH` defaults to `/data/glance.db`, so set it to somewhere writable.

## Track a site

Paste the snippet, then visit the site. That is the whole integration.

**Custom events** from your page:

```js
glance('signup')
glance('download', { plan: 'pro' })   // properties are accepted and ignored for now
```

**Single-page apps** are handled: the snippet re-sends on `pushState` and `popstate`.

**Testing locally.** Pages served from `localhost`, `127.0.0.1`, `*.localhost`, `*.local`, `*.test` or a private LAN address are accepted for every site, so you can try the snippet on a dev server before deploying. Anything else must match the site's domain or a subdomain of it. Run with `GLANCE_LOG_LEVEL=debug` to see why an event was dropped.

**What is dropped.** Known bot user agents (anything with `bot`, `crawl`, `spider`, `headless`, `curl`, `wget` and friends), browsers with `navigator.webdriver` set, events whose page host does not match the site, and bodies over 4 KB. The endpoint always answers 202 so it cannot be used to probe which site ids exist.

## Dashboard

The index lists every site with its favicon, a 14-day sparkline and this week's visitors against last week's. Drag the grip to reorder.

Per site:

- **Visitors, page views, views per visitor** with the change versus the previous equal window, over 24h, 7d, 30d or 90d.
- **Chart**: hourly for 24h and 7d, daily for 30d and 90d, with the dark tooltip on hover.
- **People in the last 30 minutes** with a per-minute strip.
- **Tabbed cards**: Pages; Sources as Referrer, Source (`utm_source`, falling back to `?ref=` and `?source=`) or Campaign (`utm_campaign`); Locations as Countries or Regions; Devices as Browsers, OS or Devices; Events. The expand icon beside a title opens the full list with a filter box. Bars grow in when a tab changes.
- **Live**: a 3D globe of visitors from the last five minutes, dots sized by count, arcs flowing from each country to your home country, refreshed every five seconds. Switch to the range map for the whole window.
- **Settings** per site: name, domain, home country, refetch favicon.

## How it works

**Visitors.** There are no cookies and no stored IPs. A visitor is `sha256(daily salt + site + IP + user agent)`, truncated to 16 hex characters. The salt rotates every UTC day and is persisted, so a restart does not split a day and nobody can be followed from one day to the next. A day's visitor count is exact; multi-day totals are the sum of daily uniques, the same convention Plausible uses.

**Country and region.** A proxy country header (`CF-IPCountry`, `X-Vercel-IP-Country`, `X-Country-Code`, `X-Geo-Country`, `CloudFront-Viewer-Country`) is used when present. Otherwise the visitor's browser time zone is mapped to a country with a table generated from the IANA zone file. "Regions" are the city part of that time zone (`Europe/London` → London), the only sub-country signal available without a GeoIP database, so there are no cities.

**Browser, OS, device.** A small ordered user-agent matcher (Edge before Chrome, Chrome before Safari, `CriOS` and `FxiOS` on iOS) plus the screen width the snippet sends. No external database.

**Referrers.** Host only, same-site traffic counts as direct, and common hosts are aliased (`t.co` → `x.com`, `www.google.com` → `google.com`, and so on).

**Rollups.** `hourly_stats` holds page views and distinct visitors per hour. `daily_stats` holds, per day, a total plus every dimension (page, referrer, country, region, device, browser, OS, event, `utm_source`, `utm_campaign`), capped at 500 keys per dimension per day with the rest folded into "Other". Raw events are pruned after the retention period; two days is the floor because today and yesterday are rebuilt from them.

**Favicons.** Glance fetches your site's icon from the site itself (declared `<link rel="icon">`, then `/favicon.ico`) and caches referrer icons the same way, so the dashboard never asks Google or a CDN. Every fetch goes through a guard that refuses loopback, private, link-local and carrier-grade NAT addresses, including after redirects, and dials the checked IP rather than the name.

**Map.** Country outlines come from Natural Earth (public domain) bundled into the UI, rendered by MapLibre with no tiles and no attribution requirement. Antarctica is left out and rings that cross the antimeridian are unwrapped so nothing fills the canvas. MapLibre loads in its own chunk after the dashboard's first paint.

**Icons.** Browser and OS marks come from [SVGL](https://svgl.app), downloaded once and bundled. Devices are small stroke glyphs. Flags are emoji; on systems that do not render them the country name still shows.

## Settings

The gear in the header opens `/settings`:

- **Overview**: sites, raw events, rollup rows, database size, uptime.
- **Appearance**: accent colour (four design swatches or any hex; it recolours the wordmark, links, chart, bars, arcs and map in both themes) and the title. Both are public so the login screen matches.
- **MCP**: the endpoint URL, an on/off switch, and API tokens. Minting shows the secret once with a ready-to-paste config block; the list shows name, prefix, created and last used, with revoke.
- **Retention**: 2 to 90 days for raw events, unless `GLANCE_RETENTION_DAYS` pins it.
- **Data**: export download, events written since start, and the dropped count if the queue ever overflowed.

## MCP (for AI agents)

Glance speaks the [Model Context Protocol](https://modelcontextprotocol.io) at `/mcp` (Streamable HTTP), read-only. Point the agent you already use at it and ask things like *"how are my sites doing this week?"*, *"did anything spike on example.com in the last month?"* or *"where is example.com's traffic coming from?"*. There is no LLM inside Glance; it serves structured numbers plus small computed signals so the agent does not have to do arithmetic on long series.

| Tool | Returns |
| --- | --- |
| `list_sites` | Every site with visitors this week and last, and visitors online now |
| `overview` | Every site over a range with the change versus the previous window, a rising/falling/flat trend, spike buckets, the peak, and the top page, referrer, country and events. The right first call |
| `site_stats` | Full detail for one site: totals, series, top 10 of every breakdown, and the same signals. Takes `filters` to narrow to matching visitors |
| `breakdown` | The complete list for any dimension, up to 500 rows. Takes the same `filters` |
| `search_terms` | Google search queries from Search Console with clicks, impressions and position, for connected sites |
| `revenue` | Polar revenue: totals, series, revenue per visitor, attributed versus unattributed orders, and revenue by first-touch referrer, source, campaign, landing page, country and product |

Ranges are `24h`, `7d`, `30d`, `90d`; words like `week` and `month` are accepted. Sites resolve by id, name, domain or a fuzzy match. `filters` is a map of dimension to key, such as `{"ref": "google.com", "country": "GB"}`; filtered answers come from raw events and say so when the range was cut to the retention window. The server instructions explain how revenue attribution is collected and why the unattributed bucket exists, so the agent does not mistake pre-attribution orders for direct sales.

Mint a token in **Settings → MCP** (or set `GLANCE_MCP_TOKEN`) and use it as a bearer token. The admin login works there too. Turning the endpoint off in Settings returns `404 mcp_disabled` to everyone.

```bash
claude mcp add --transport http glance https://glance.example.com/mcp --header "Authorization: Bearer glance_tok_…"
```

Any client that supports Streamable HTTP with a custom header can connect the same way.

Things to ask once it is connected:

- *"Give me an overview of all my sites for the last 30 days."*
- *"Which referrer sent the most visitors to example.com this week, and is it growing?"*
- *"Show me example.com's traffic from Germany over the last month."* (uses `filters`)
- *"What did people search on Google to find example.com?"* (needs [Search Console](#google-search-terms))
- *"How much revenue came from Product Hunt visitors?"* (needs [Polar](#revenue-from-polar))

## API

All endpoints are under `/api/v1`. Errors are JSON: `{"error": "code", "message": "..."}`.

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| GET | `/health` | none | `{"status":"ok"}` |
| GET | `/glance.js` | none | The tracking snippet, cached for a day |
| POST | `/api/v1/collect` | none | Ingest one event `{s, n, u, r, w, tz}`; always 202 |
| GET | `/api/v1/theme` | none | `{accent, title}` for the UI |
| GET/POST | `/api/v1/sites` | admin | List (with 7-day card and live count) / create `{name?, domain}` |
| GET/PATCH/DELETE | `/api/v1/sites/:id` | admin | Manage `{name?, domain?, home_country?}` |
| POST | `/api/v1/sites/reorder` | admin | `{ids}` in display order |
| GET | `/api/v1/sites/:id/stats?range=` | admin | Totals, previous window, series and top-10 breakdowns |
| GET | `/api/v1/sites/:id/breakdown?dim=&range=&limit=` | admin | Full list for one dimension, up to 500 rows |
| GET | `/api/v1/sites/:id/live` | admin | Last 5 minutes by country, per-minute visitors for the last 30 minutes |
| GET | `/api/v1/sites/:id/favicon` | admin | The stored site icon |
| POST | `/api/v1/sites/:id/refresh-favicon` | admin | Refetch it now |
| GET | `/api/v1/favicon?host=` | admin | A cached referrer icon |
| POST | `/api/v1/rollup` | admin | Flush the queue and rebuild today's rollups now |
| GET | `/api/v1/status` | admin | Counts, database size, uptime, ingest stats |
| GET/PATCH | `/api/v1/settings` | admin | `accent`, `title`, `mcp_enabled`, `retention_days` |
| GET/POST | `/api/v1/tokens` | admin | List / mint `{name}` (returns `secret` once) |
| DELETE | `/api/v1/tokens/:id` | admin | Revoke |
| GET | `/api/v1/export` | admin | Every site and daily rollup as one JSON file |
| POST | `/mcp` | token or admin | Read-only [MCP](#mcp-for-ai-agents) endpoint |

**Admin auth.** Set `GLANCE_ADMIN_USER` and `GLANCE_ADMIN_PASSWORD` and the UI shows a sign-in screen; admin endpoints then need the session cookie it sets (`POST /api/v1/auth/login`) or HTTP Basic credentials (`curl -u user:pass …`). Sessions last 30 days, survive restarts, and are invalidated when the password changes. Leave both unset and everything is open. Only do that behind your own proxy, Tailscale or VPN.

**API tokens** (`glance_tok_…`) are minted in Settings, stored as hashes, and accepted for `/mcp` and every `GET` admin endpoint. They get `403 read_only` on anything that writes.

## Configuration

| Variable | Default | Notes |
| --- | --- | --- |
| `GLANCE_PORT` | `8080` | |
| `GLANCE_DATABASE_PATH` | `/data/glance.db` | WAL mode, migrations applied on start |
| `GLANCE_RETENTION_DAYS` | `7` | Days of raw events to keep, minimum 2. When set it pins the value; leave unset to manage it from Settings. Rollups are kept forever |
| `GLANCE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. JSON logs on stdout |
| `GLANCE_ADMIN_USER` | | Dashboard username; set together with the password |
| `GLANCE_ADMIN_PASSWORD` | | 8+ characters. Unset = no login |
| `GLANCE_MCP_TOKEN` | | Optional fixed bearer token for the [MCP endpoint](#mcp-for-ai-agents); 16+ characters. Tokens minted in Settings work without it |
| `GLANCE_GOOGLE_CLIENT_ID` | | OAuth client for [Google Search Console](#google-search-terms); set together with the secret |
| `GLANCE_GOOGLE_CLIENT_SECRET` | | |

Glance reads the client IP from `X-Forwarded-For` (or `X-Real-IP`), which Traefik and most proxies set. The IP is only ever hashed.

## Google search terms

Google strips the query from the referrer, so the only way to see which searches bring people in is the Search Console API. Each site's settings panel has a **Connect Google Search Console** button once an OAuth client is configured. Glance pulls the last 16 months on connect and refreshes once a day; the dashboard gets a **Search terms** card and the MCP server a `search_terms` tool. Google's data trails by two to three days.

1. In [Google Cloud](https://console.cloud.google.com/apis/credentials), create a project, enable the **Google Search Console API**, and create an **OAuth client ID** of type *Web application*.
2. Add the redirect URI shown in the site's settings panel, `https://glance.example.com/api/v1/google/callback`. It must match exactly.
3. Set `GLANCE_GOOGLE_CLIENT_ID` and `GLANCE_GOOGLE_CLIENT_SECRET` and restart.
4. On the OAuth consent screen, add your Google account as a test user, or publish the app. While the app is in *Testing*, Google expires the grant after seven days and Glance shows **Reconnect Google**; publishing removes that limit (no verification needed for your own use, you just click through an "unverified app" warning once).
5. Open a site, **Settings**, **Connect Google Search Console**. Glance picks the property matching the domain, preferring a domain property (`sc-domain:`) over URL-prefix ones, and asks you to choose if none matches.

Only the `webmasters.readonly` scope is requested. The refresh token is stored in the SQLite file alongside everything else; disconnecting revokes it with Google and deletes the stored terms.

## Filtering

Click any row in Pages, Sources, Locations, Devices or Events to narrow the whole dashboard to the visitors who matched it: the chart, the totals and every other card. Filters stack, sit in the URL so they can be shared, and clear with one click. Because filtered views are built from raw events rather than rollups, they only reach back as far as raw events are kept: with the default 7 days, a 30d range under a filter shows the last week and says so. Raise retention in Settings if you want to filter longer ranges. The MCP `site_stats` and `breakdown` tools take the same `filters`.

## Revenue from Polar

If you sell through [Polar](https://polar.sh), each site can show revenue next to traffic, the way datafa.st does with Stripe. Open a site, **Settings**, **Polar**, and paste an organization access token (Polar dashboard, Settings, Developers; the `orders:read` scope is enough). Glance pulls two years of orders, then reconciles once a day. Add a webhook in Polar pointing at the URL shown in the panel, subscribed to the `order.*` events, and paste its secret so sales appear within seconds. If the organization sells several products, list the product ids that belong to this site so the others are ignored.

The dashboard gets revenue, orders and revenue per visitor tiles, revenue bars behind the traffic chart, and a **Revenue** card broken down by first-touch referrer, source, campaign, landing page, country and product. Revenue is the net amount after discounts and before tax, less refunds, in the currency you charge in. The MCP server gains a `revenue` tool.

### Attributing sales to a source

Polar only knows what your checkout tells it. Add `data-attribution` to the snippet and it remembers each visitor's first referrer and landing URL in their own browser (`localStorage`, never sent to Glance, no cookie):

```html
<script defer src="https://glance.example.com/glance.js" data-site="site_…" data-attribution></script>
```

When you create a Polar checkout, read `glance.attribution()` (it returns `{ r: referrer, l: landing URL, t: timestamp }` or `null`) and pass `attr_ref` and `attr_landing` in the checkout `metadata`. Glance normalises them with the same rules as page views, so "Revenue by source" agrees with "Sources". Orders placed before you wire this up count towards totals but show as unattributed.

## Deploying with Dokploy (or any compose host)

Use `docker-compose.dokploy.yml`, not `docker-compose.yml`. It swaps the `./data` bind mount for a named volume (the bind mount is created root-owned on the host, and the image runs as uid 1000, so SQLite cannot write `/data/glance.db`), joins the external `dokploy-network` so Traefik can route to it, and drops the published port so the server is reachable only through your HTTPS proxy.

1. New **Compose** application → your repo, compose path `docker-compose.dokploy.yml`.
2. **Environment** tab: `GLANCE_ADMIN_USER`, `GLANCE_ADMIN_PASSWORD`, optionally `GLANCE_MCP_TOKEN`. Dokploy writes these to a `.env` beside the compose file, which is what `env_file` picks up.
3. Add a domain with HTTPS in Dokploy pointing at port `8080`; deploy.
4. Use that domain in the snippet: `https://glance.example.com/glance.js`.

Editing the compose file in Dokploy's UI only works for **Raw** compose apps; when the source is Git, commit changes and redeploy.

## Footprint

The image is about 37 MB and idles at under 10 MB of memory. Storage is dominated by rollups: one row per site per day per breakdown key, so a site with a hundred pages and a dozen referrers adds a few hundred rows a day. Raw events are the only thing that grows with traffic, and they are pruned.

## Development

```bash
cd server && GLANCE_DATABASE_PATH=./data/glance.db go run ./cmd/glance   # API on :8080
cd server/web && npm install && npm run dev                               # UI on :5173, proxies /api
make test                                                                 # Go + web tests
make build                                                                # bin/glance with the UI embedded
```

Requires Go 1.27 and Node 24 (see `.tool-versions`). The SQLite driver is pure Go, so `CGO_ENABLED=0` builds work everywhere. The Go tests cover ingest through rollup to stats with a fixed clock, day-scoped hashing, the user-agent table, timezone mapping, the SSRF guard, favicon parsing, the MCP tools over a real client, tokens and settings.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Releases are listed in [CHANGELOG.md](CHANGELOG.md); security reports go through [SECURITY.md](SECURITY.md).

## Licence

MIT.
