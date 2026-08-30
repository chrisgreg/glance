// Package events buffers incoming events and writes them to SQLite in
// batches, so the request path never touches the database.
package events

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrisgreg/glance/server/internal/ids"
)

// Kinds.
const (
	KindPageview = "pageview"
	KindEvent    = "event"
)

// Event is one enriched, anonymous hit.
type Event struct {
	SiteID  string
	At      time.Time
	Kind    string
	Name    string
	Path    string
	RefHost string
	Country string
	Device  string
	Browser string
	OS      string
	Region  string
	UTMSrc  string
	UTMCamp string
	Visitor string
}

// VisitorHash derives the day-scoped anonymous visitor id. The IP and user
// agent go in, only a truncated hash comes out, and the salt changes daily.
func VisitorHash(salt, siteID, ip, ua string) string {
	sum := sha256.Sum256([]byte(salt + "|" + siteID + "|" + ip + "|" + ua))
	return hex.EncodeToString(sum[:8])
}

const (
	bufferSize    = 4096
	flushEvery    = time.Second
	flushAtCount  = 200
	dropLogPeriod = time.Minute
)

// Writer batches events into the database.
type Writer struct {
	db  *sql.DB
	log *slog.Logger
	ch  chan Event
	wg  sync.WaitGroup

	dropped  atomic.Int64
	lastDrop atomic.Int64 // unix seconds
	Written  atomic.Int64
	flushReq chan chan error
	running  atomic.Bool
}

// NewWriter returns a Writer; call Start to begin flushing.
func NewWriter(db *sql.DB, log *slog.Logger) *Writer {
	return &Writer{db: db, log: log, ch: make(chan Event, bufferSize), flushReq: make(chan chan error)}
}

// Enqueue adds an event without blocking. When the buffer is full the event
// is dropped and counted.
func (w *Writer) Enqueue(e Event) {
	select {
	case w.ch <- e:
	default:
		n := w.dropped.Add(1)
		now := time.Now().Unix()
		if last := w.lastDrop.Load(); now-last >= int64(dropLogPeriod.Seconds()) && w.lastDrop.CompareAndSwap(last, now) {
			w.log.Warn("events.dropped", "total", n)
		}
	}
}

// Start runs the flush loop until ctx is cancelled, then drains what is left.
func (w *Writer) Start(ctx context.Context) {
	w.wg.Add(1)
	w.running.Store(true)
	go func() {
		defer w.wg.Done()
		defer w.running.Store(false)
		batch := make([]Event, 0, flushAtCount)
		t := time.NewTicker(flushEvery)
		defer t.Stop()
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			err := w.write(batch)
			if err != nil {
				w.log.Error("events.write_failed", "count", len(batch), "error", err.Error())
			}
			batch = batch[:0]
			return err
		}
		for {
			select {
			case reply := <-w.flushReq:
				// Drain the channel into the batch too, then write everything.
				for drained := false; !drained; {
					select {
					case e := <-w.ch:
						batch = append(batch, e)
					default:
						drained = true
					}
				}
				reply <- flush()
			case <-ctx.Done():
				// Drain anything still queued.
				for {
					select {
					case e := <-w.ch:
						batch = append(batch, e)
						if len(batch) >= flushAtCount {
							flush()
						}
					default:
						flush()
						return
					}
				}
			case e := <-w.ch:
				batch = append(batch, e)
				if len(batch) >= flushAtCount {
					flush()
				}
			case <-t.C:
				flush()
			}
		}
	}()
}

// Wait blocks until the flush loop has exited.
func (w *Writer) Wait() { w.wg.Wait() }

// Flush writes everything queued right now, including events the loop has
// already taken into its pending batch. Works with or without Start.
func (w *Writer) Flush() error {
	if w.running.Load() {
		reply := make(chan error, 1)
		select {
		case w.flushReq <- reply:
			return <-reply
		case <-time.After(5 * time.Second):
			return context.DeadlineExceeded
		}
	}
	var batch []Event
	for {
		select {
		case e := <-w.ch:
			batch = append(batch, e)
		default:
			if len(batch) == 0 {
				return nil
			}
			return w.write(batch)
		}
	}
}

func (w *Writer) write(batch []Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO events (site_id, ts, kind, name, path, ref_host, country, device, browser, os, region, utm_source, utm_campaign, visitor) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range batch {
		if _, err := stmt.ExecContext(ctx, e.SiteID, ids.Format(e.At), e.Kind, e.Name, e.Path, e.RefHost, e.Country, e.Device, e.Browser, e.OS, e.Region, e.UTMSrc, e.UTMCamp, e.Visitor); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	w.Written.Add(int64(len(batch)))
	return nil
}

// Dropped returns how many events were discarded because the buffer was full.
func (w *Writer) Dropped() int64 { return w.dropped.Load() }

// Prune deletes raw events older than days.
func Prune(ctx context.Context, db *sql.DB, days int, now time.Time) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM events WHERE ts < ?`, ids.Format(now.AddDate(0, 0, -days)))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
