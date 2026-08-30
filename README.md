# Glance

A tiny, self-hosted web analytics service in the same family as Boop and UP. Visitors, page views, top pages, referrers, countries, devices and simple custom events, at a glance. Nothing heavier than that.

One Go binary, one SQLite file, one Docker container. Sites add a cookieless snippet under 1 KB. No sessions, funnels, cohorts, heatmaps or replay, and no third-party services at all: favicons are fetched by Glance itself and the world map ships its own outlines.

## What is in the box

| Part | Where |
| --- | --- |
| Go server (collect endpoint, batched writer, rollups, favicons, embedded web UI) | `server/` |
| Web UI (Svelte 5, LayerChart, MapLibre, built into the binary) | `server/web/` |

- `/` lists your websites with a sparkline and this week's visitors. Add sites and copy the tracking code there.
- `/s/{id}` is the dashboard: visitors, page views and views per visitor with deltas, a 24h/7d/30d/90d chart, a "people in the last 30 minutes" strip, tabbed cards (Pages; Sources as referrer, `utm_source`/`?ref=` or `utm_campaign`; Locations as countries or regions; Devices as browsers, OS or device class; Events), each expandable to the full list, plus a live 3D globe with arcs from each visiting country to your home country and a range map.
- `/glance.js` is the snippet, `/api/v1/collect` the endpoint it posts to. Both are public; everything else is behind the login.

## Quick start (Docker)

```bash
cp .env.example .env          # set GLANCE_ADMIN_USER and GLANCE_ADMIN_PASSWORD
mkdir -p data && chown 1000:1000 data   # Linux hosts only; the container runs as uid 1000
docker compose up -d --build
open http://localhost:8082
```

Add a website, then paste the snippet before `</head>`:

```html
<script defer src="https://glance.example.com/glance.js" data-site="site_…"></script>
```

Testing locally: pages served from `localhost`, `127.0.0.1`, `*.localhost`, `*.local`, `*.test` or a private LAN address are accepted for every site, so you can try the snippet on a dev server before deploying. Anything else must match the site's domain or a subdomain of it. Run with `GLANCE_LOG_LEVEL=debug` to see why an event was dropped.

Custom events from your page:

```js
glance('signup')
```

Data lives in `./data/glance.db`. Back up by copying that file. For Dokploy or any Traefik-fronted host use `docker-compose.dokploy.yml` and set the environment variables in the host's UI.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `GLANCE_ADMIN_USER` / `GLANCE_ADMIN_PASSWORD` | unset | Login for the dashboard and admin API. Both or neither; password 8+ characters. Sessions survive restarts and are invalidated when the password changes. |
| `GLANCE_PORT` | `8080` | Listen port. |
| `GLANCE_DATABASE_PATH` | `/data/glance.db` | SQLite file. |
| `GLANCE_RETENTION_DAYS` | `7` | Days of raw events to keep, minimum 2. Hourly and daily rollups are kept forever. |
| `GLANCE_MCP_TOKEN` | unset | Optional fixed bearer token for the MCP endpoint, 16+ characters. Tokens minted in Settings and the admin login work there too. |
| `GLANCE_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. JSON logs on stdout. |

## How it works

**Collecting.** The snippet sends one small JSON body per page view (site id, URL, referrer, screen width, time zone) with `sendBeacon`, and again on `pushState` and `popstate` for single-page apps. The server validates the URL host against the site's domain, drops known bots, derives everything it needs, and pushes the event onto an in-memory queue. The request never touches the database. A writer goroutine commits the queue every second or every 200 events in one transaction.

**Visitors.** There are no cookies and no stored IPs. A visitor is `sha256(daily salt + site + IP + user agent)`, truncated. The salt rotates every UTC day, so a visitor cannot be followed from one day to the next. A day's visitor count is exact; multi-day totals are the sum of daily uniques, the same convention Plausible uses.

**Country.** A proxy country header (`CF-IPCountry`, `X-Vercel-IP-Country`, `X-Country-Code`) is used when present. Otherwise the visitor's browser time zone is mapped to a country with an embedded table. That is approximate, but it needs no GeoIP database.

**Regions and campaigns.** "Regions" are the city part of the visitor's time zone ("Europe/London" → London), the only sub-country signal available without GeoIP, so there are no cities. Sources and campaigns come from `utm_source` (falling back to `?ref=` or `?source=`) and `utm_campaign` on the landing URL.

**Rollups.** Every five minutes today's and yesterday's rollups are rebuilt from raw events: pageviews and visitors per hour, and per day for each dimension (page, referrer, country, region, device, browser, OS, event, utm_source, utm_campaign, capped at 500 keys with the rest folded into "Other"). The dashboard reads only rollups, so it stays fast however much traffic you have. Raw events are pruned after `GLANCE_RETENTION_DAYS`.

**Favicons.** Glance fetches your site's icon from the site itself (declared `<link rel="icon">`, then `/favicon.ico`) and caches referrer icons the same way, so the dashboard makes no requests to Google or any CDN. Every fetch goes through a guard that refuses private, loopback and link-local addresses, including on redirects.

**Map.** Country outlines come from Natural Earth (public domain) bundled into the UI. No tile server, no attribution, no runtime network. Arcs run from each country's centroid to the site's home country (set it in Settings; blank uses your top country). Flags are emoji; on systems that do not render them the country name still shows.

## Settings

The gear in the header opens `/settings`: overview numbers (sites, raw events, rollup rows, database size, uptime), the accent colour (four design swatches or any hex; it recolours the wordmark, links, chart, bars and map in both themes), the title, the MCP switch, API tokens, raw-event retention (2 to 90 days, unless `GLANCE_RETENTION_DAYS` pins it) and the JSON export.

**API tokens** are minted on the settings page and shown once; only a hash is stored. They open the MCP endpoint and every `GET` on the admin API, and are refused for anything that writes. Revoking one takes effect immediately.

## Ask an agent

Glance serves a read-only [MCP](https://modelcontextprotocol.io) endpoint at `/mcp` (Streamable HTTP). Mint a token in Settings (or set `GLANCE_MCP_TOKEN`) and point your agent at it:

```json
{
  "mcpServers": {
    "glance": {
      "url": "https://glance.example.com/mcp",
      "headers": { "Authorization": "Bearer your-token" }
    }
  }
}
```

Tools: `list_sites`, `overview` (every site over a range with the change versus the previous window, a rising/falling/flat trend, spike buckets and the top page, referrer and country), `site_stats` (full detail and series for one site) and `breakdown` (the complete list for any dimension). Ranges are 24h, 7d, 30d and 90d. Ask things like "how are my sites doing this week", "did anything spike on uini.io in the last month" or "where is chrisgregori.dev's traffic coming from".

## Icons

Browser and operating system marks come from [SVGL](https://svgl.app) and are bundled at build time. Site and referrer favicons are fetched by Glance itself. Nothing on the dashboard loads from a third party at runtime.

## API

Public:

- `GET /glance.js`
- `POST /api/v1/collect` `{s, n, u, r, w, tz}` (always 202)
- `GET /health`

Admin (session cookie from `POST /api/v1/auth/login`, or HTTP Basic):

- `GET /api/v1/sites`, `POST /api/v1/sites` `{name?, domain}`, `GET|PATCH|DELETE /api/v1/sites/{id}`, `POST /api/v1/sites/reorder` `{ids}`
- `GET /api/v1/sites/{id}/stats?range=24h|7d|30d|90d`
- `GET /api/v1/sites/{id}/breakdown?dim=page|ref|country|region|device|browser|os|event|utm_source|utm_campaign&range=&limit=` (up to 500 rows)
- `GET /api/v1/sites/{id}/live` (last 5 minutes by country, per-minute visitors for the last 30 minutes)
- `POST /api/v1/rollup` (flush the queue and rebuild today's rollups now)
- `POST|GET /mcp` (MCP, bearer `GLANCE_MCP_TOKEN` or admin login)
- `GET /api/v1/sites/{id}/favicon`, `POST /api/v1/sites/{id}/refresh-favicon`, `GET /api/v1/favicon?host=`
- `GET /api/v1/status`, `GET /api/v1/export` (sites plus every daily rollup as JSON)
- `GET|PATCH /api/v1/settings` `{accent?, title?, mcp_enabled?, retention_days?}`, `GET /api/v1/theme` (public: accent and title)
- `GET|POST /api/v1/tokens`, `DELETE /api/v1/tokens/{id}`

## Development

```bash
make web && make run     # build the UI, then serve on :8080 with ./data/glance.db
make dev                 # Vite on :5173, proxying /api to :8080
make test                # go vet + go test, svelte-check + vitest
```
