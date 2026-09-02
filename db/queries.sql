-- name: UpsertPeer :one
INSERT INTO peers (public_key, allowed_ips, first_seen, last_seen)
VALUES ($1, $2, now(), now())
ON CONFLICT (public_key) DO UPDATE SET last_seen = now(), allowed_ips = $2
RETURNING id, track_stats, last_rx_bytes, last_tx_bytes;

-- name: UpdatePeerCounters :exec
UPDATE peers SET last_rx_bytes = $2, last_tx_bytes = $3 WHERE id = $1;

-- name: InsertSnapshot :exec
INSERT INTO peer_snapshots
    (peer_id, ts, endpoint, latest_handshake, rx_bytes, tx_bytes, rx_delta, tx_delta)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: SetPeerTracking :exec
-- Включить/выключить запись статистики для пира вручную (peer_snapshots не
-- пишется, если track_stats = false).
UPDATE peers SET track_stats = $2 WHERE public_key = $1;
