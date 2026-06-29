-- Run against the PeerDB server (psql postgres://localhost:9900) to create the source/target peers
-- and the CDC mirror. Hostnames are docker-compose service names (PeerDB runs inside the network).

CREATE PEER source_pg FROM POSTGRES WITH (
  host = 'source-postgres',
  port = 5432,
  user = 'postgres',
  password = 'postgres',
  database = 'source'
);

CREATE PEER target_ch FROM CLICKHOUSE WITH (
  host = 'clickhouse',
  port = 9000,
  user = 'default',
  password = 'pass',
  database = 'default',
  disable_tls = true
);

-- CDC mirror: snapshot the existing rows, then stream changes. PeerDB creates the destination table
-- `events` in ClickHouse with its bookkeeping columns (_peerdb_is_deleted, _peerdb_version, ...).
CREATE MIRROR events_mirror
  FROM source_pg TO target_ch
  WITH TABLE MAPPING (public.events:events)
  WITH (do_initial_copy = true);
