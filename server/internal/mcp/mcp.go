// Package mcp exposes Glance's analytics to AI agents over the Model Context
// Protocol. Every tool is read-only. Besides raw numbers the tools return
// small computed signals (deltas, trend, spikes) so an agent can answer
// "how are my sites doing" without doing arithmetic on long series.
package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chrisgreg/glance/server/internal/sites"
	"github.com/chrisgreg/glance/server/internal/stats"
)

// Stores is what the tools read from.
type Stores struct {
	Sites *sites.Store
	Stats *stats.Store
	Now   func() time.Time
}

// NewServer builds the MCP server with every tool registered.
func NewServer(st Stores, version string) *sdk.Server {
	if st.Now == nil {
		st.Now = time.Now
	}
	s := sdk.NewServer(&sdk.Implementation{Name: "glance", Title: "Glance", Version: version}, &sdk.ServerOptions{
		Instructions: "Glance is a self-hosted, cookieless web analytics service tracking the owner's websites. These tools are read-only. " +
			"Ranges are 24h, 7d, 30d or 90d and always end now. Visitors are daily uniques (multi-day totals sum daily uniques); pageviews are raw counts. " +
			"Every stats result includes a comparison with the equal window before it (delta_pct), a trend (second half of the window vs the first), and spikes (buckets far above the window's mean). " +
			"Start with overview for all sites at once, then site_stats for detail and breakdown for full lists of pages, referrers, countries, devices, browsers, operating systems or events. " +
			"Sites can be referred to by id, name or domain.",
	})
	t := &tools{st: st}
	ro := &sdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolPtr(false)}

	sdk.AddTool(s, &sdk.Tool{Name: "list_sites", Title: "List sites", Annotations: ro,
		Description: "List every tracked website with its id, name, domain, visitors this week and last week, and visitors online right now."}, t.listSites)
	sdk.AddTool(s, &sdk.Tool{Name: "overview", Title: "Overview of all sites", Annotations: ro,
		Description: "Totals for every site over a range with the change versus the previous equal window, a trend, spike buckets, and the top page, referrer and country. The best first call for questions like 'how are my sites doing this week'."}, t.overview)
	sdk.AddTool(s, &sdk.Tool{Name: "site_stats", Title: "Site stats", Annotations: ro,
		Description: "Full detail for one site over a range: totals, previous window, delta, trend, spikes, the time series (hourly for 24h and 7d, daily for 30d and 90d) and the top 10 of every breakdown."}, t.siteStats)
	sdk.AddTool(s, &sdk.Tool{Name: "breakdown", Title: "Breakdown", Annotations: ro,
		Description: "The full list for one dimension of one site over a range, best first: page, ref (referrer host, empty = direct), country (ISO code, empty = unknown), region (time-zone city), device, browser, os, event, utm_source or utm_campaign."}, t.breakdown)
	return s
}

// Handler returns an HTTP handler speaking Streamable HTTP. Authentication
// is the caller's job (wrap it).
func Handler(s *sdk.Server, log *slog.Logger) http.Handler {
	return sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s }, &sdk.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, Logger: log,
		DisableLocalhostProtection: true,
	})
}

type tools struct{ st Stores }

func (t *tools) resolve(ctx context.Context, ref string) (sites.Site, error) {
	ref = strings.TrimSpace(strings.ToLower(ref))
	if ref == "" {
		return sites.Site{}, fmt.Errorf("site is required (id, name or domain)")
	}
	list, err := t.st.Sites.List(ctx)
	if err != nil {
		return sites.Site{}, err
	}
	for _, s := range list {
		if strings.ToLower(s.ID) == ref || strings.ToLower(s.Domain) == ref || strings.ToLower(s.Name) == ref {
			return s, nil
		}
	}
	for _, s := range list {
		if strings.Contains(strings.ToLower(s.Name), ref) || strings.Contains(strings.ToLower(s.Domain), ref) {
			return s, nil
		}
	}
	return sites.Site{}, fmt.Errorf("no site matches %q; call list_sites", ref)
}

func normRange(r string) (string, error) {
	r = strings.ToLower(strings.TrimSpace(r))
	switch r {
	case "":
		return "7d", nil
	case "24h", "1d", "day", "today":
		return "24h", nil
	case "7d", "week", "1w":
		return "7d", nil
	case "30d", "month", "1m":
		return "30d", nil
	case "90d", "quarter", "3m":
		return "90d", nil
	}
	if stats.ValidRange(r) {
		return r, nil
	}
	return "", fmt.Errorf("range must be one of 24h, 7d, 30d, 90d")
}

// ---- analysis ----

// Signals are the computed summaries attached to every stats answer.
type Signals struct {
	VisitorsDeltaPct  *float64 `json:"visitors_delta_pct" jsonschema:"percent change in visitors versus the previous equal window; null when the previous window had none"`
	PageviewsDeltaPct *float64 `json:"pageviews_delta_pct"`
	Trend             string   `json:"trend" jsonschema:"rising, falling or flat: visitors in the second half of the window versus the first"`
	TrendPct          float64  `json:"trend_pct" jsonschema:"percent change from the first half of the window to the second"`
	Spikes            []Spike  `json:"spikes" jsonschema:"buckets whose visitors were far above the window's mean"`
	PeakBucket        string   `json:"peak_bucket,omitempty" jsonschema:"start of the busiest bucket"`
	PeakVisitors      int      `json:"peak_visitors,omitempty"`
	ViewsPerVisitor   float64  `json:"views_per_visitor"`
}

// Spike is one unusually busy bucket.
type Spike struct {
	Bucket   string  `json:"bucket" jsonschema:"bucket start, RFC 3339 UTC"`
	Visitors int     `json:"visitors"`
	Ratio    float64 `json:"ratio_to_mean" jsonschema:"visitors divided by the window mean"`
}

func pct(now, prev int) *float64 {
	if prev == 0 {
		return nil
	}
	v := math.Round(float64(now-prev)/float64(prev)*1000) / 10
	return &v
}

// Analyse derives Signals from a summary.
func Analyse(sum stats.Summary) Signals {
	sig := Signals{VisitorsDeltaPct: pct(sum.Totals.Visitors, sum.Previous.Visitors), PageviewsDeltaPct: pct(sum.Totals.Pageviews, sum.Previous.Pageviews), Spikes: []Spike{}, Trend: "flat"}
	if sum.Totals.Visitors > 0 {
		sig.ViewsPerVisitor = math.Round(float64(sum.Totals.Pageviews)/float64(sum.Totals.Visitors)*100) / 100
	}
	n := len(sum.Series)
	if n == 0 {
		return sig
	}
	var total, first, second int
	for i, p := range sum.Series {
		total += p.Visitors
		if i < n/2 {
			first += p.Visitors
		} else {
			second += p.Visitors
		}
		if p.Visitors > sig.PeakVisitors {
			sig.PeakVisitors, sig.PeakBucket = p.Visitors, p.T
		}
	}
	if first > 0 {
		sig.TrendPct = math.Round(float64(second-first)/float64(first)*1000) / 10
		switch {
		case sig.TrendPct >= 15:
			sig.Trend = "rising"
		case sig.TrendPct <= -15:
			sig.Trend = "falling"
		}
	} else if second > 0 {
		sig.Trend, sig.TrendPct = "rising", 100
	}
	mean := float64(total) / float64(n)
	var variance float64
	for _, p := range sum.Series {
		d := float64(p.Visitors) - mean
		variance += d * d
	}
	sd := math.Sqrt(variance / float64(n))
	for _, p := range sum.Series {
		if mean > 0 && p.Visitors >= 5 && float64(p.Visitors) > mean+2*sd && float64(p.Visitors) > 2*mean {
			sig.Spikes = append(sig.Spikes, Spike{Bucket: p.T, Visitors: p.Visitors, Ratio: math.Round(float64(p.Visitors)/mean*10) / 10})
		}
	}
	sort.Slice(sig.Spikes, func(i, j int) bool { return sig.Spikes[i].Visitors > sig.Spikes[j].Visitors })
	if len(sig.Spikes) > 5 {
		sig.Spikes = sig.Spikes[:5]
	}
	return sig
}

// ---- list_sites ----

type ListSitesIn struct{}

type SiteOut struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	VisitorsWeek int    `json:"visitors_this_week"`
	VisitorsPrev int    `json:"visitors_last_week"`
	Online       int    `json:"online_now" jsonschema:"distinct visitors seen in the last five minutes"`
}

type ListSitesOut struct {
	Sites []SiteOut `json:"sites"`
}

func (t *tools) listSites(ctx context.Context, _ *sdk.CallToolRequest, _ ListSitesIn) (*sdk.CallToolResult, ListSitesOut, error) {
	list, err := t.st.Sites.List(ctx)
	if err != nil {
		return nil, ListSitesOut{}, err
	}
	now := t.st.Now()
	out := ListSitesOut{Sites: []SiteOut{}}
	for _, s := range list {
		card, err := t.st.Stats.Card(ctx, s.ID, now)
		if err != nil {
			return nil, ListSitesOut{}, err
		}
		live, _ := t.st.Stats.LiveVisitors(ctx, s.ID, now)
		out.Sites = append(out.Sites, SiteOut{ID: s.ID, Name: s.Name, Domain: s.Domain, VisitorsWeek: card.Visitors, VisitorsPrev: card.Previous, Online: live})
	}
	return nil, out, nil
}

// ---- overview ----

type OverviewIn struct {
	Range string `json:"range,omitempty" jsonschema:"24h, 7d, 30d or 90d; default 7d"`
}

type SiteOverview struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Domain      string      `json:"domain"`
	Visitors    int         `json:"visitors"`
	Pageviews   int         `json:"pageviews"`
	PrevVisits  int         `json:"previous_visitors"`
	PrevViews   int         `json:"previous_pageviews"`
	Signals     Signals     `json:"signals"`
	TopPage     *stats.Row  `json:"top_page,omitempty"`
	TopReferrer *stats.Row  `json:"top_referrer,omitempty" jsonschema:"empty key means direct"`
	TopCountry  *stats.Row  `json:"top_country,omitempty"`
	TopEvents   []stats.Row `json:"top_events,omitempty"`
}

type OverviewOut struct {
	Range    string         `json:"range"`
	From     string         `json:"from"`
	To       string         `json:"to"`
	Totals   stats.Totals   `json:"totals" jsonschema:"all sites combined"`
	Previous stats.Totals   `json:"previous"`
	Sites    []SiteOverview `json:"sites" jsonschema:"busiest first"`
}

func firstRow(rows []stats.Row) *stats.Row {
	if len(rows) == 0 {
		return nil
	}
	r := rows[0]
	return &r
}

func (t *tools) overview(ctx context.Context, _ *sdk.CallToolRequest, in OverviewIn) (*sdk.CallToolResult, OverviewOut, error) {
	rng, err := normRange(in.Range)
	if err != nil {
		return nil, OverviewOut{}, err
	}
	list, err := t.st.Sites.List(ctx)
	if err != nil {
		return nil, OverviewOut{}, err
	}
	now := t.st.Now()
	out := OverviewOut{Range: rng, Sites: []SiteOverview{}}
	for _, s := range list {
		sum, err := t.st.Stats.Summary(ctx, s.ID, rng, now, 3)
		if err != nil {
			return nil, OverviewOut{}, err
		}
		out.From, out.To = sum.From, sum.To
		out.Totals.Visitors += sum.Totals.Visitors
		out.Totals.Pageviews += sum.Totals.Pageviews
		out.Previous.Visitors += sum.Previous.Visitors
		out.Previous.Pageviews += sum.Previous.Pageviews
		out.Sites = append(out.Sites, SiteOverview{
			ID: s.ID, Name: s.Name, Domain: s.Domain,
			Visitors: sum.Totals.Visitors, Pageviews: sum.Totals.Pageviews, PrevVisits: sum.Previous.Visitors, PrevViews: sum.Previous.Pageviews,
			Signals: Analyse(sum), TopPage: firstRow(sum.Breakdowns["page"]), TopReferrer: firstRow(sum.Breakdowns["ref"]), TopCountry: firstRow(sum.Breakdowns["country"]), TopEvents: sum.Breakdowns["event"],
		})
	}
	sort.SliceStable(out.Sites, func(i, j int) bool { return out.Sites[i].Visitors > out.Sites[j].Visitors })
	return nil, out, nil
}

// ---- site_stats ----

type SiteStatsIn struct {
	Site  string `json:"site" jsonschema:"site id, name or domain"`
	Range string `json:"range,omitempty" jsonschema:"24h, 7d, 30d or 90d; default 7d"`
}

type SiteStatsOut struct {
	Site    SiteOut       `json:"site"`
	Stats   stats.Summary `json:"stats"`
	Signals Signals       `json:"signals"`
}

func (t *tools) siteStats(ctx context.Context, _ *sdk.CallToolRequest, in SiteStatsIn) (*sdk.CallToolResult, SiteStatsOut, error) {
	s, err := t.resolve(ctx, in.Site)
	if err != nil {
		return nil, SiteStatsOut{}, err
	}
	rng, err := normRange(in.Range)
	if err != nil {
		return nil, SiteStatsOut{}, err
	}
	now := t.st.Now()
	sum, err := t.st.Stats.Summary(ctx, s.ID, rng, now, 10)
	if err != nil {
		return nil, SiteStatsOut{}, err
	}
	card, _ := t.st.Stats.Card(ctx, s.ID, now)
	live, _ := t.st.Stats.LiveVisitors(ctx, s.ID, now)
	return nil, SiteStatsOut{Site: SiteOut{ID: s.ID, Name: s.Name, Domain: s.Domain, VisitorsWeek: card.Visitors, VisitorsPrev: card.Previous, Online: live}, Stats: sum, Signals: Analyse(sum)}, nil
}

// ---- breakdown ----

type BreakdownIn struct {
	Site  string `json:"site" jsonschema:"site id, name or domain"`
	Dim   string `json:"dim" jsonschema:"page, ref, country, region, device, browser, os, event, utm_source or utm_campaign"`
	Range string `json:"range,omitempty" jsonschema:"24h, 7d, 30d or 90d; default 7d"`
	Limit int    `json:"limit,omitempty" jsonschema:"1-500, default 100"`
}

type BreakdownOut struct {
	Site  string      `json:"site"`
	Dim   string      `json:"dim"`
	Range string      `json:"range"`
	Rows  []stats.Row `json:"rows" jsonschema:"best first; visitors are daily uniques summed over the range"`
}

func (t *tools) breakdown(ctx context.Context, _ *sdk.CallToolRequest, in BreakdownIn) (*sdk.CallToolResult, BreakdownOut, error) {
	s, err := t.resolve(ctx, in.Site)
	if err != nil {
		return nil, BreakdownOut{}, err
	}
	rng, err := normRange(in.Range)
	if err != nil {
		return nil, BreakdownOut{}, err
	}
	dim := strings.ToLower(strings.TrimSpace(in.Dim))
	switch dim {
	case "pages":
		dim = "page"
	case "referrer", "referrers", "refs":
		dim = "ref"
	case "countries":
		dim = "country"
	case "devices":
		dim = "device"
	case "browsers":
		dim = "browser"
	case "events":
		dim = "event"
	case "regions", "cities", "city":
		dim = "region"
	case "source", "sources", "utm", "utm_sources":
		dim = "utm_source"
	case "campaign", "campaigns", "utm_campaigns":
		dim = "utm_campaign"
	}
	if !stats.ValidDim(dim) {
		return nil, BreakdownOut{}, fmt.Errorf("dim must be one of %s", strings.Join(stats.Dims, ", "))
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := t.st.Stats.Breakdown(ctx, s.ID, dim, rng, t.st.Now(), limit)
	if err != nil {
		return nil, BreakdownOut{}, err
	}
	return nil, BreakdownOut{Site: s.Name, Dim: dim, Range: rng, Rows: rows}, nil
}

func boolPtr(b bool) *bool { return &b }
