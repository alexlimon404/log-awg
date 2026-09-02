// Command log-awg is a daemon that polls `awg show <iface> dump` on a
// self-correcting minute interval and records peer activity into Postgres.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"log-awg/internal/awg"
	"log-awg/internal/store"
)

type config struct {
	iface        string
	bin          string
	dsn          string
	execTimeout  time.Duration
	pollInterval time.Duration
}

func loadConfig() (config, error) {
	cfg := config{
		iface:        getenv("AWG_INTERFACE", "awg0"),
		bin:          getenv("AWG_BIN", "awg"),
		dsn:          os.Getenv("DATABASE_URL"),
		execTimeout:  5 * time.Second,
		pollInterval: time.Minute,
	}
	if cfg.dsn == "" {
		return cfg, fmt.Errorf("DATABASE_URL is required")
	}
	if v := os.Getenv("AWG_EXEC_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid AWG_EXEC_TIMEOUT: %w", err)
		}
		cfg.execTimeout = d
	}
	if v := os.Getenv("AWG_POLL_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid AWG_POLL_INTERVAL: %w", err)
		}
		cfg.pollInterval = d
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.dsn)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	defer st.Close()

	log.Printf("log-awg started: interface=%s poll_interval=%s", cfg.iface, cfg.pollInterval)
	runLoop(ctx, cfg, st)
	log.Print("log-awg stopped")
}

// runLoop wakes up on every boundary of cfg.pollInterval (e.g. every exact
// minute) rather than using a plain time.Ticker, so it self-corrects instead
// of drifting, and never runs two polls concurrently since each iteration
// waits for the previous runOnce to finish.
func runLoop(ctx context.Context, cfg config, st *store.Store) {
	for {
		now := time.Now()
		next := now.Truncate(cfg.pollInterval).Add(cfg.pollInterval)

		select {
		case <-ctx.Done():
			return
		case <-time.After(next.Sub(now)):
			runOnce(ctx, cfg, st)
		}
	}
}

func runOnce(ctx context.Context, cfg config, st *store.Store) {
	execCtx, cancel := context.WithTimeout(ctx, cfg.execTimeout)
	peers, err := awg.Show(execCtx, cfg.bin, cfg.iface)
	cancel()
	if err != nil {
		log.Printf("awg show failed, skipping this tick: %v", err)
		return
	}

	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	saved, skipped, err := st.SaveSnapshot(dbCtx, peers)
	cancel()
	if err != nil {
		log.Printf("save snapshot failed, skipping this tick: %v", err)
		return
	}

	log.Printf("snapshot saved: %d peers seen, %d written, %d skipped", len(peers), saved, skipped)
}
