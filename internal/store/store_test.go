package store

import (
	"context"
	"os"
	"testing"
	"time"

	"log-awg/internal/awg"
)

// TestSaveSnapshot exercises SaveSnapshot against a real Postgres instance.
// It is skipped unless DATABASE_URL is set (see docs/PLAN.md for how to spin
// one up locally, e.g. via docker run postgres + `migrate ... up`).
func TestSaveSnapshot(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	trackedKey := "tracked-" + t.Name()
	untrackedKey := "untracked-" + t.Name()

	mustExec(t, ctx, s, `DELETE FROM peers WHERE public_key IN ($1, $2)`, trackedKey, untrackedKey)
	mustExec(t, ctx, s, `INSERT INTO peers (public_key, track_stats) VALUES ($1, false)`, untrackedKey)

	hs := time.Now().Add(-30 * time.Second)
	peers := []awg.Peer{
		{
			PublicKey:       trackedKey,
			Endpoint:        "1.2.3.4:51820",
			AllowedIPs:      []string{"10.12.0.10/32"},
			HasHandshake:    true,
			LatestHandshake: hs,
			RxBytes:         1000,
			TxBytes:         2000,
		},
		{
			PublicKey:       untrackedKey,
			Endpoint:        "5.6.7.8:51820",
			HasHandshake:    true,
			LatestHandshake: hs,
			RxBytes:         500,
			TxBytes:         700,
		},
		{
			// never connected: must be skipped regardless of track_stats
			PublicKey:    "neverconnected-" + t.Name(),
			HasHandshake: false,
		},
	}

	saved, skipped, err := s.SaveSnapshot(ctx, peers)
	if err != nil {
		t.Fatalf("SaveSnapshot (first): %v", err)
	}
	if saved != 1 {
		t.Errorf("expected 1 saved snapshot, got %d", saved)
	}
	if skipped != 2 {
		t.Errorf("expected 2 skipped, got %d", skipped)
	}

	var allowedIPs string
	if err := s.pool.QueryRow(ctx, `SELECT allowed_ips FROM peers WHERE public_key = $1`, trackedKey).Scan(&allowedIPs); err != nil {
		t.Fatalf("query allowed_ips: %v", err)
	}
	if allowedIPs != "10.12.0.10/32" {
		t.Errorf("expected allowed_ips %q, got %q", "10.12.0.10/32", allowedIPs)
	}

	var rxDelta, txDelta int64
	err = s.pool.QueryRow(ctx, `
		SELECT rx_delta, tx_delta FROM peer_snapshots ps
		JOIN peers p ON p.id = ps.peer_id
		WHERE p.public_key = $1
	`, trackedKey).Scan(&rxDelta, &txDelta)
	if err != nil {
		t.Fatalf("query snapshot: %v", err)
	}
	if rxDelta != 1000 || txDelta != 2000 {
		t.Errorf("expected first-poll delta to equal raw counters (1000, 2000), got (%d, %d)", rxDelta, txDelta)
	}

	var untrackedSnapshots int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM peer_snapshots ps
		JOIN peers p ON p.id = ps.peer_id
		WHERE p.public_key = $1
	`, untrackedKey).Scan(&untrackedSnapshots); err != nil {
		t.Fatalf("query untracked snapshots: %v", err)
	}
	if untrackedSnapshots != 0 {
		t.Errorf("expected no snapshots for track_stats=false peer, got %d", untrackedSnapshots)
	}

	// second poll: rx/tx grew, delta should be the increase, not the raw total
	peers[0].RxBytes = 1600
	peers[0].TxBytes = 2900
	saved, skipped, err = s.SaveSnapshot(ctx, peers)
	if err != nil {
		t.Fatalf("SaveSnapshot (second): %v", err)
	}
	if saved != 1 || skipped != 2 {
		t.Fatalf("second poll: expected saved=1 skipped=2, got saved=%d skipped=%d", saved, skipped)
	}

	err = s.pool.QueryRow(ctx, `
		SELECT rx_delta, tx_delta FROM peer_snapshots ps
		JOIN peers p ON p.id = ps.peer_id
		WHERE p.public_key = $1
		ORDER BY ts DESC LIMIT 1
	`, trackedKey).Scan(&rxDelta, &txDelta)
	if err != nil {
		t.Fatalf("query second snapshot: %v", err)
	}
	if rxDelta != 600 || txDelta != 900 {
		t.Errorf("expected delta (600, 900) on second poll, got (%d, %d)", rxDelta, txDelta)
	}
}

func mustExec(t *testing.T, ctx context.Context, s *Store, sql string, args ...any) {
	t.Helper()
	if _, err := s.pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}
