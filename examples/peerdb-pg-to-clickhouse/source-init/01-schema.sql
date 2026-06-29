-- Source table for the PeerDB Postgres -> ClickHouse example.
-- The `config` JSONB column intentionally stores objects whose keys are in different orders across
-- rows: that's the case where a naive MD5(json::text) comparison between source and destination
-- breaks, and where Seatbelt's row mapper earns its keep.

CREATE TABLE events (
    id          BIGINT PRIMARY KEY,
    user_id     INTEGER,
    name        VARCHAR(100),
    email       VARCHAR(100),
    status      VARCHAR(20),
    created_at  TIMESTAMP,
    score       NUMERIC(10,2),
    description TEXT,
    config      JSONB
);
