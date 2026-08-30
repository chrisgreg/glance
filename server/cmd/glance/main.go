// Command glance runs the Glance analytics server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/chrisgreg/glance/server/internal/api"
	"github.com/chrisgreg/glance/server/internal/auth"
	"github.com/chrisgreg/glance/server/internal/config"
	"github.com/chrisgreg/glance/server/internal/database"
	"github.com/chrisgreg/glance/server/internal/events"
	"github.com/chrisgreg/glance/server/internal/favicons"
	"github.com/chrisgreg/glance/server/internal/ids"
	"github.com/chrisgreg/glance/server/internal/rollup"
	"github.com/chrisgreg/glance/server/internal/settings"
	"github.com/chrisgreg/glance/server/internal/sites"
	"github.com/chrisgreg/glance/server/internal/stats"
	"github.com/chrisgreg/glance/server/internal/tokens"
	"github.com/chrisgreg/glance/server/internal/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "glance:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)

	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database %s: %w", cfg.DatabasePath, err)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	admin := auth.NewAdmin(cfg.AdminUser, cfg.AdminPassword, auth.NewSessionStore(db))
	if cfg.MCPToken != "" {
		log.Info("mcp.enabled", "endpoint", "/mcp")
	}
	if admin.Enabled() {
		log.Info("auth.enabled", "user", cfg.AdminUser)
	} else {
		log.Warn("auth.disabled", "hint", "set GLANCE_ADMIN_USER and GLANCE_ADMIN_PASSWORD to protect the dashboard")
	}

	writer := events.NewWriter(db, log)
	writer.Start(ctx)
	siteStore := sites.New(db)
	fetcher := favicons.New(db)

	st := settings.New(db)
	srv := &api.Server{
		DB: db, Log: log, Sites: siteStore, Settings: st, Writer: writer, Stats: stats.New(db),
		Favicons: fetcher, Admin: admin, Web: web.Handler(), TrustProxy: true, MCPToken: cfg.MCPToken,
		Tokens: tokens.New(db), RetentionDays: cfg.RetentionDays, RetentionFromEnv: cfg.RetentionDaysSet,
		StartedAt: time.Now(), DatabasePath: cfg.DatabasePath,
	}

	go maintenance(ctx, log, db, siteStore, st, fetcher, cfg)

	httpServer := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(cfg.Port)),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()
	log.Info("server.started", "port", cfg.Port, "database", cfg.DatabasePath, "version", api.Version)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	log.Info("server.stopping")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	stop()
	writer.Wait()
	// Final rollup so nothing written in the last minutes is lost to the UI.
	_ = rollup.Run(context.Background(), db, log, time.Now())
	return nil
}

// maintenance rolls up every minute, prunes raw events hourly and refreshes
// site favicons weekly.
func maintenance(ctx context.Context, log *slog.Logger, db *sqlDB, siteStore *sites.Store, st *settings.Store, fetcher *favicons.Fetcher, cfg config.Config) {
	roll := func() {
		if err := rollup.Run(ctx, db, log, time.Now()); err != nil && ctx.Err() == nil {
			log.Error("rollup.failed", "error", err.Error())
		}
	}
	prune := func() {
		days := cfg.RetentionDays
		if g, err := st.General(ctx, cfg.RetentionDays, cfg.RetentionDaysSet); err == nil {
			days = g.RetentionDays
		}
		n, err := events.Prune(ctx, db, days, time.Now())
		if err != nil && ctx.Err() == nil {
			log.Error("prune.failed", "error", err.Error())
		} else if n > 0 {
			log.Info("prune.completed", "deleted", n)
		}
		_ = auth.NewSessionStore(db).Prune(ctx, time.Now())
		stale, err := siteStore.StaleFavicons(ctx, ids.Format(time.Now().AddDate(0, 0, -7)))
		if err != nil {
			return
		}
		for _, st := range stale {
			fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			data, ctype, err := fetcher.ForDomain(fctx, st.Domain)
			cancel()
			if err != nil {
				_ = siteStore.SetFavicon(ctx, st.ID, nil, "")
				continue
			}
			_ = siteStore.SetFavicon(ctx, st.ID, data, ctype)
		}
	}
	roll()
	prune()
	rt := time.NewTicker(time.Minute)
	pt := time.NewTicker(time.Hour)
	defer rt.Stop()
	defer pt.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-rt.C:
			roll()
		case <-pt.C:
			prune()
		}
	}
}

type sqlDB = database.DB

func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
