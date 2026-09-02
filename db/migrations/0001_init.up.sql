CREATE TABLE peers (
    id             BIGSERIAL PRIMARY KEY,
    public_key     TEXT NOT NULL UNIQUE,
    name           TEXT,
    track_stats    BOOLEAN NOT NULL DEFAULT TRUE,
    first_seen     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_rx_bytes  BIGINT NOT NULL DEFAULT 0,
    last_tx_bytes  BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE peer_snapshots (
    id                BIGSERIAL PRIMARY KEY,
    peer_id           BIGINT NOT NULL REFERENCES peers(id) ON DELETE CASCADE,
    ts                TIMESTAMPTZ NOT NULL DEFAULT now(),
    endpoint          TEXT,
    latest_handshake  TIMESTAMPTZ NOT NULL,
    rx_bytes          BIGINT NOT NULL,
    tx_bytes          BIGINT NOT NULL,
    rx_delta          BIGINT NOT NULL,
    tx_delta          BIGINT NOT NULL
);

CREATE INDEX peer_snapshots_peer_ts_idx ON peer_snapshots (peer_id, ts DESC);
CREATE INDEX peer_snapshots_ts_idx ON peer_snapshots (ts);
