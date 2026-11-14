package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"haven.local/backend/internal/httpapi"
	"haven.local/backend/internal/intelligence"
	"haven.local/backend/internal/rentals"
	"haven.local/backend/internal/store"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("haven stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	envFile := flag.String("env-file", ".env", "local server environment file")
	mode := flag.String("mode", "api", "api, worker, migrate or import")
	fixture := flag.String("file", "", "path to permitted observation JSON for import mode")
	workers := flag.Int("workers", 2, "concurrent worker loops (1–8)")
	flag.Parse()
	if err := loadEnv(*envFile); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var repo store.Repository
	var pg *store.Postgres
	repo = store.Unavailable{}
	dsn := os.Getenv("HAVEN_DATABASE_URL")
	if dsn != "" {
		boot, cancel := context.WithTimeout(ctx, 10*time.Second)
		var err error
		pg, err = store.Open(boot, dsn, os.Getenv("HAVEN_REDIS_URL"))
		cancel()
		if err != nil {
			return err
		}
		defer pg.Close()
		repo = pg
	} else if *mode != "api" {
		return fmt.Errorf("HAVEN_DATABASE_URL required for %s mode", *mode)
	}

	allowed := map[string]bool{}
	for _, s := range strings.Split(os.Getenv("HAVEN_ALLOWED_SOURCES"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			allowed[s] = true
		}
	}
	if *mode == "migrate" {
		return pg.Migrate(ctx)
	}
	if *mode == "import" {
		if *fixture == "" {
			return fmt.Errorf("-file must point to permitted observations")
		}
		file, err := os.ReadFile(*fixture)
		if err != nil {
			return err
		}
		var observations []intelligence.Observation
		if err = json.Unmarshal(file, &observations); err != nil {
			return err
		}
		// Validate the whole import before the first insert. Production imports
		// remain replayable if a later storage operation fails.
		for _, o := range observations {
			if err = o.Validate(time.Now()); err != nil {
				return err
			}
			if !allowed[o.Source] {
				return fmt.Errorf("source %q is not allowlisted", o.Source)
			}
		}
		for _, o := range observations {
			if _, err = repo.Insert(ctx, o); err != nil {
				return err
			}
		}
		if *mode == "import" {
			slog.Info("observations imported", "count", len(observations))
			return nil
		}
	}
	if *mode == "worker" {
		if *workers < 1 || *workers > 8 {
			return fmt.Errorf("workers must be 1–8")
		}
		var wg sync.WaitGroup
		for i := 0; i < *workers; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); pg.RunWorker(ctx) }()
		}
		wg.Wait()
		return nil
	}
	if *mode != "api" {
		return fmt.Errorf("unknown mode %q", *mode)
	}
	token := os.Getenv("HAVEN_IMPORT_TOKEN")
	if pg != nil && (len(token) < 32 || len(allowed) == 0) {
		return fmt.Errorf("API requires HAVEN_IMPORT_TOKEN of at least 32 characters and HAVEN_ALLOWED_SOURCES")
	}
	origin := os.Getenv("HAVEN_WEB_ORIGIN")
	if origin == "" {
		origin = "http://127.0.0.1:5173"
	}
	addr := os.Getenv("HAVEN_LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	server := &http.Server{Addr: addr, Handler: (&httpapi.API{Store: repo, ImportToken: token, AllowedSources: allowed, Origin: origin, Live: rentals.New(os.Getenv("HAVEN_RENTCAST_API_KEY"))}).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 16}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	slog.Info("Haven listening", "address", addr, "live_search_configured", os.Getenv("HAVEN_RENTCAST_API_KEY") != "")
	select {
	case err := <-done:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
