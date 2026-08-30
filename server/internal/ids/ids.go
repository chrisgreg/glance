// Package ids generates prefixed, random, URL-safe identifiers such as evt_x7k3....
package ids

import (
	"crypto/rand"
	"encoding/base32"
	"time"
)

var enc = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// New returns prefix + "_" + 16 random base32 characters (80 bits of entropy).
func New(prefix string) string {
	return prefix + "_" + Random(10)
}

// Random returns n random bytes encoded as lowercase base32.
func Random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("ids: crypto/rand failed: " + err.Error())
	}
	return enc.EncodeToString(b)
}

// TimeLayout is a fixed-width UTC timestamp layout. Fixed width keeps
// lexical ordering equal to chronological ordering, which cursor pagination relies on.
const TimeLayout = "2006-01-02T15:04:05.000000000Z"

// Now returns the current UTC time formatted with TimeLayout.
func Now() string { return Format(time.Now()) }

// Format formats t in UTC with TimeLayout.
func Format(t time.Time) string { return t.UTC().Format(TimeLayout) }

// Parse parses a TimeLayout or RFC3339 timestamp.
func Parse(s string) (time.Time, error) {
	if t, err := time.Parse(TimeLayout, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}
