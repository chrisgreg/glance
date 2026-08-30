package mcp

import (
	"testing"

	"github.com/chrisgreg/glance/server/internal/stats"
)

func TestAnalyse(t *testing.T) {
	series := []stats.Point{}
	for i := 0; i < 14; i++ {
		v := 10
		if i >= 7 {
			v = 20 // second week doubles
		}
		if i == 10 {
			v = 120 // one spike
		}
		series = append(series, stats.Point{T: "d" + string(rune('a'+i)), Visitors: v, Pageviews: v * 2})
	}
	sum := stats.Summary{Totals: stats.Totals{Visitors: 310, Pageviews: 620}, Previous: stats.Totals{Visitors: 200, Pageviews: 500}, Series: series}
	sig := Analyse(sum)
	if sig.VisitorsDeltaPct == nil || *sig.VisitorsDeltaPct != 55 {
		t.Fatalf("delta: %v", sig.VisitorsDeltaPct)
	}
	if sig.Trend != "rising" {
		t.Fatalf("trend: %s (%v%%)", sig.Trend, sig.TrendPct)
	}
	if len(sig.Spikes) != 1 || sig.Spikes[0].Bucket != "dk" || sig.Spikes[0].Visitors != 120 {
		t.Fatalf("spikes: %+v", sig.Spikes)
	}
	if sig.PeakVisitors != 120 || sig.ViewsPerVisitor != 2 {
		t.Fatalf("peak/ratio: %d %v", sig.PeakVisitors, sig.ViewsPerVisitor)
	}
	flat := Analyse(stats.Summary{Series: []stats.Point{{Visitors: 5}, {Visitors: 5}, {Visitors: 5}, {Visitors: 5}}})
	if flat.Trend != "flat" || len(flat.Spikes) != 0 || flat.VisitorsDeltaPct != nil {
		t.Fatalf("flat: %+v", flat)
	}
}

func TestNormRange(t *testing.T) {
	for in, want := range map[string]string{"": "7d", "week": "7d", "24H": "24h", "month": "30d", "90d": "90d"} {
		if got, err := normRange(in); err != nil || got != want {
			t.Fatalf("%q: %q %v", in, got, err)
		}
	}
	if _, err := normRange("year"); err == nil {
		t.Fatal("year should be rejected")
	}
}
