package stats

import (
	"testing"
	"time"
)

func TestWindow(t *testing.T) {
	now := time.Date(2026, 3, 4, 15, 42, 0, 0, time.UTC)
	for _, c := range []struct {
		rng    string
		from   string
		to     string
		bucket string
	}{
		// The hour-bucketed short ranges end at the current hour.
		{"24h", "2026-03-03T16:00:00Z", "2026-03-04T16:00:00Z", "hour"},
		{"48h", "2026-03-02T16:00:00Z", "2026-03-04T16:00:00Z", "hour"},
		// The rest run to the end of today.
		{"7d", "2026-02-26T00:00:00Z", "2026-03-05T00:00:00Z", "hour"},
		{"30d", "2026-02-03T00:00:00Z", "2026-03-05T00:00:00Z", "day"},
		{"90d", "2025-12-05T00:00:00Z", "2026-03-05T00:00:00Z", "day"},
		{"180d", "2025-09-06T00:00:00Z", "2026-03-05T00:00:00Z", "day"},
	} {
		from, to, bucket := Window(c.rng, now)
		if from.Format(time.RFC3339) != c.from || to.Format(time.RFC3339) != c.to || bucket != c.bucket {
			t.Errorf("%s: got %s..%s/%s, want %s..%s/%s", c.rng, from.Format(time.RFC3339), to.Format(time.RFC3339), bucket, c.from, c.to, c.bucket)
		}
	}
}

func TestValidRange(t *testing.T) {
	for _, r := range Ranges {
		if !ValidRange(r) {
			t.Errorf("%s should be valid", r)
		}
	}
	for _, r := range []string{"", "12h", "1y", "48H"} {
		if ValidRange(r) {
			t.Errorf("%s should not be valid", r)
		}
	}
}
