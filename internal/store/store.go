// Package store persists parsed awg peer state into Postgres via sqlc-generated
// queries (internal/sqlcgen).
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"log-awg/internal/awg"
	"log-awg/internal/sqlcgen"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

// SaveSnapshot upserts every peer seen in the dump and updates its traffic
// counters. A peer_snapshots row is written only for peers that have
// track_stats=true AND have completed at least one handshake — peers with
// track_stats=false are tracked in the peers table but their activity is
// never recorded.
func (s *Store) SaveSnapshot(ctx context.Context, peers []awg.Peer) (saved, skipped int, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	q := sqlcgen.New(tx)
	now := time.Now()

	for _, p := range peers {
		row, err := q.UpsertPeer(ctx, p.PublicKey)
		if err != nil {
			return 0, 0, fmt.Errorf("upsert peer %s: %w", p.PublicKey, err)
		}

		if err := q.UpdatePeerCounters(ctx, sqlcgen.UpdatePeerCountersParams{
			ID:          row.ID,
			LastRxBytes: int64(p.RxBytes),
			LastTxBytes: int64(p.TxBytes),
		}); err != nil {
			return 0, 0, fmt.Errorf("update counters for peer %s: %w", p.PublicKey, err)
		}

		if !row.TrackStats || !p.HasHandshake {
			skipped++
			continue
		}

		var endpoint *string
		if p.Endpoint != "" {
			endpointVal := p.Endpoint
			endpoint = &endpointVal
		}

		if err := q.InsertSnapshot(ctx, sqlcgen.InsertSnapshotParams{
			PeerID:          row.ID,
			Ts:              pgtype.Timestamptz{Time: now, Valid: true},
			Endpoint:        endpoint,
			LatestHandshake: pgtype.Timestamptz{Time: p.LatestHandshake, Valid: true},
			RxBytes:         int64(p.RxBytes),
			TxBytes:         int64(p.TxBytes),
			RxDelta:         delta(row.LastRxBytes, int64(p.RxBytes)),
			TxDelta:         delta(row.LastTxBytes, int64(p.TxBytes)),
		}); err != nil {
			return 0, 0, fmt.Errorf("insert snapshot for peer %s: %w", p.PublicKey, err)
		}
		saved++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("commit tx: %w", err)
	}
	return saved, skipped, nil
}

// delta returns the traffic increase since the previous poll. A counter
// that went down means the interface or peer was recreated and the
// underlying wg counter reset to zero, so the new value is the delta.
func delta(oldValue, newValue int64) int64 {
	if newValue < oldValue {
		return newValue
	}
	return newValue - oldValue
}
