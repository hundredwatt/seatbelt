# Architecture: how the Go tool works

This document traces what actually happens when you run `seatbelt run -c config.yaml`. Code lives
under [`../seatbelt`](../seatbelt).

## Components

A validation run wires together three pluggable pieces, selected from the connection-string schemes
in your `config.yaml`:

| Role | Interface | Implementations |
|------|-----------|-----------------|
| **Source** | `seatbelt.Source` (`pkg/seatbelt/components.go`) | PostgreSQL (`pkg/postgres`), MySQL (`pkg/mysql`, batch-only) |
| **Target** | `seatbelt.Target` | ClickHouse (`pkg/clickhouse`), PostgreSQL (`pkg/postgres`) |
| **Row mapper + hashers** | `seatbelt.RowMapperAndHasher` | `peer_db` (WASM, `pkg/row_mappers`), `generic` (identity) |

`cmd/seatbelt/main.go` `createComponents` builds them: `postgres://` → Postgres, `mysql://` → MySQL,
`clickhouse://` → ClickHouse.

## The hashing model

The key idea is that source and destination hashes are computed by **different** functions and are
**not** expected to be equal:

- **Source hash.** `PostgresSourceHasher` builds a SQL expression
  `COALESCE(col::text, '👻') || …` and hashes it with PostgreSQL's native
  `hashtextextended` (a fast, signed 64-bit hash). This runs *in the database* during a `COPY`, so the
  only thing crossing the wire is `(pk, hash)` per row.
- **Destination hash.** `ClickHouseTargetHasher` builds the analogous ClickHouse expression and hashes
  it with `xxh3` (unsigned 64-bit), again computed in-database.
- **The bridge.** A **row mapper** transforms a source row into the same *canonical string* the
  destination would produce, so Seatbelt can compute the expected destination hash from source data
  and reconcile the two hash domains. For PeerDB → ClickHouse this is the `peer_db` WASM mapper; for a
  1:1 copy (e.g. MySQL → Postgres via Sling) it's the `generic` identity mapper.

## The flow

`pkg/seatbelt/fetch_data.go` has two modes:

### Live (default) — `defaultFetchData`

1. **Source scan** (`Source.Scan`): a single `COPY (SELECT pk, hashtextextended(...)) TO STDOUT`
   streams `(pk, source_hash)` — minimal source load.
2. **Target scan** (`Target.Scan`): `SELECT pk, xxh3(...)` streams `(pk, target_hash)`.
3. **Change stream** (`Source.StartChangeStreamConsumer`): a PostgreSQL logical-replication consumer
   (`pkg/postgres/postgres_change_stream_consumer.go`) reads the WAL via `pgoutput`, reproduces both
   the source and destination hashes for every changed row (running the row mapper), and writes the
   `source_hash → target_hash` map. Slot/publication come from `replication.slot_name` /
   `replication.publication_name` (defaults `seatbelt_slot` / `seatbelt_pub`).

### Batch — `initialLoad` (`--initial-load`)

For pipelines without a usable change stream (e.g. a MySQL source, or a one-off check):
`Source.ExtractScan` reads the rows and produces `(pk, source_hash, target_hash)` directly, and
`Target.Scan` produces `(pk, target_hash)`. There's no operation history, so discrepancies surface as
**Pending** rather than **Error** (see [`01-concepts.md`](./01-concepts.md)).

## The shadow table (triangulation in DuckDB)

`pkg/seatbelt/shadow.go` loads the scan files into an embedded **DuckDB** database as views and runs
the reconciliation in SQL. The persistent `shadow` table carries, per pk, the latest source/target
signatures and operations. The incremental update calls the Seatbelt SQL functions —
`determine_source_operation_*`, `check_for_validation_error_with_row_integrity`, etc. — provided by
the [DuckDB extension](../duckdb-seatbelt-extension). Those functions are the same logic specified in
[`../change-validation-core`](../change-validation-core). The run prints
Valid / Pending / Error counts.

## Row mappers are pluggable (WASM)

The mapper is the only pipeline-specific piece, and it's a WebAssembly module loaded at runtime
(`pkg/row_mappers/wasm_mapper.go`, via wazero). You can ship a mapper for any pipeline, in any
language that targets `wasm32`, without rebuilding Seatbelt. The contract is
[`../wasm-mappers/ABI.md`](../wasm-mappers/ABI.md); the reference `peer_db` mapper is a small Zig
program in [`../wasm-mappers/peerdb`](../wasm-mappers/peerdb).

## Known limitations

- Single-column **integer** primary keys (the shadow casts `pk` to `BIGINT`).
- MySQL is supported as a **batch** source only (no change stream).
- First-class pipelines are PeerDB (PG → ClickHouse) and Sling (MySQL → PG); anything else needs a
  mapper.
