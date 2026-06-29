# Setup: validating a PeerDB Postgres → ClickHouse pipeline

This walks through validating a [PeerDB](https://www.peerdb.io) CDC pipeline that replicates a
Postgres table to ClickHouse, and how to write a mapper for a pipeline Seatbelt doesn't already know.
The runnable version is [`../examples/peerdb-pg-to-clickhouse`](../examples/peerdb-pg-to-clickhouse).

## When to use this

- Your pipeline replicates **Postgres → ClickHouse** (PeerDB or a compatible CDC tool).
- You want a full all-rows / all-columns audit with minimal load on the source.

## 1. Build the binary

```bash
cd seatbelt && go build -o bin/seatbelt ./cmd/seatbelt
```

## 2. Write the config

```yaml
source_connection_string: "postgres://user:pass@pg-host:5432/source"
target_connection_string: "clickhouse://default:pass@ch-host:9000/default?password=pass"

row_mapper_name: "peer_db"        # WASM mapper that knows PeerDB's ClickHouse representation

table_name: "public.events"
target_table_name: "default.events"    # PeerDB's destination table
primary_key_name: "id"

columns:
  - {name: user_id,    source_type: integer,                     target_type: Int32}
  - {name: name,       source_type: character varying,           target_type: String}
  - {name: created_at, source_type: timestamp without time zone, target_type: DateTime64(6)}
  - {name: score,      source_type: numeric,                     target_type: Decimal(10,2)}
  - {name: config,     source_type: jsonb,                       target_type: String}

# Optional: only needed for the live (non --initial-load) change-stream path.
replication:
  slot_name: "seatbelt_slot"
  publication_name: "seatbelt_pub"

seatbelt_data_path: "tmp/shadow.db"
environment:
  SEATBELT_TEMP_DIR: "tmp/"
```

`source_type` / `target_type` are the Postgres and ClickHouse column types; the `peer_db` mapper uses
them to reproduce PeerDB's canonical string (JSON re-serialization, decimal/timestamp handling, the
`_peerdb_is_deleted` soft-delete column).

## 3. Run the validation

Quick batch check:

```bash
./bin/seatbelt run -c config.yaml --initial-load
```

For a **live** audit, drop `--initial-load`. Seatbelt then consumes the Postgres change log via a
logical-replication slot/publication (the `replication:` block above) and applies the full Data Change
Validation rules, so genuine discrepancies are promoted from **Pending** to **Error** instead of being
confused with replication lag.

```
--- Validation Metrics ---
Source Row Count   3,218,936
Target Row Count   3,218,936
Valid Rows         3,218,935
Pending Rows               1
Error Rows                 0
```

(Real output validating a 3.2 M-row, 4 GB table with a `jsonb` column — the ClickHouse scan read only
~2.6% of the table's size.)

## Writing a mapper for another pipeline

Seatbelt only knows a pipeline through its **row mapper** — the component that reproduces how the
pipeline transforms a row. Mappers are WebAssembly modules loaded at runtime, so you can add support
for a new pipeline without touching the Seatbelt binary:

1. Read [`../wasm-mappers/ABI.md`](../wasm-mappers/ABI.md). A mapper exports `set_schema`,
   `transform_source`, `transform_target`, plus allocation helpers, and speaks a small JSON wire
   format.
2. Implement `transform_source` (Postgres row → canonical string) and `transform_target` (destination
   row → canonical string) so that equal data yields equal strings. Copy the Zig reference mapper in
   [`../wasm-mappers/peerdb`](../wasm-mappers/peerdb).
3. Compile to `wasm32` and load it via `NewWasmMapperFromBinary`. **Verify byte-for-byte parity
   against the reference** for your column types before relying on it — a mapper bug looks exactly
   like a pipeline bug.

The hard part of any mapper is the columns that don't compare cleanly — JSON key ordering,
floating-point, decimals, timestamps. That's precisely the work Hash Triangulation pushes into one
reproducible place instead of scattering it across every comparison.
