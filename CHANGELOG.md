# Changelog

All notable changes to Glance are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/).

Pushing a `v*` tag builds the Docker image and binaries and creates a GitHub
release with the matching section of this file as its notes.

## [Unreleased]

## [1.0.0] - 2026-09-04

First public release.

### Added

- Cookieless tracking snippet (about 1 KB) with `sendBeacon`, single-page-app
  support via `pushState` and `popstate`, and custom events through
  `glance('name')`.
- Collect endpoint that validates the page host against the site, drops bots
  and automated browsers, always answers 202, and batches writes through an
  in-memory queue.
- Privacy model: no cookies, no stored IPs; visitors are a daily-salted hash
  of site, IP and user agent.
- Enrichment without external services: browser, OS and device from the user
  agent, country from proxy headers or the browser time zone, region from the
  time zone city, referrer aliasing, `utm_source` and `utm_campaign`.
- Hourly and daily rollups rebuilt every minute and on demand, raw-event
  retention from 2 to 90 days, pruning.
- Svelte dashboard: site list with sparklines and drag to reorder; per-site
  totals with change versus the previous window over 24h, 7d, 30d and 90d;
  chart; live visitors; tabbed breakdown cards for pages, sources, locations,
  devices and events; a 3D live globe and a range map; per-site settings.
- Filtering: click any breakdown row to narrow the whole dashboard, stack
  filters, share them in the URL.
- Favicons fetched from the site itself and cached, behind an SSRF guard.
- Settings: accent colour and title, MCP on/off, API tokens, retention, data
  export, status overview.
- Admin login with persistent sessions, HTTP Basic support, and read-only API
  tokens.
- Read-only MCP endpoint at `/mcp` (Streamable HTTP) with `list_sites`,
  `overview`, `site_stats`, `breakdown`, `search_terms` and `revenue` tools.
- Google Search Console integration for search terms.
- Polar integration for revenue next to traffic, with first-touch attribution
  through `data-attribution` and `glance.attribution()`.
- Single static binary with the UI embedded, Docker image on
  `ghcr.io/chrisgreg/glance`, compose files for local use and Dokploy.

[Unreleased]: https://github.com/chrisgreg/glance/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/chrisgreg/glance/releases/tag/v1.0.0
