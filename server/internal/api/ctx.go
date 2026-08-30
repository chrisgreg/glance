package api

import (
	stdctx "context"
	"time"
)

// context returns a short-lived background context for work that outlives
// the request, such as favicon fetches.
func bgContext() (stdctx.Context, stdctx.CancelFunc) {
	return stdctx.WithTimeout(stdctx.Background(), 15*time.Second)
}
