# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Seatbelt is an open-source toolkit for **validating live, high-throughput CDC pipelines**. It
introduces two primitives:

- **Data Change Validation** — detects a broad class of pipeline failures (missing rows, missing
  updates, extra rows, duplicates) by tracking row-level operations over time, with no per-table or
  per-column configuration.
- **Hash Triangulation** — a full source-to-destination comparison of all rows/columns with minimal
  source load, computed asynchronously from the source's change log so it tolerates live changes and
  pipeline transformations.

## Repository Layout

| Path | What it is |
|------|------------|
| `seatbelt/` | Core Go program. Computes source hashes via PostgreSQL `hashtextextended`, consumes logical replication to recompute matching hashes, and triangulates source↔destination in DuckDB. |
| `duckdb-seatbelt-extension/` | DuckDB extension exposing the data-change-validation functions as SQL. Contains vendored `duckdb` + `extension-ci-tools` submodules. |
| `wasm-mappers/` | Language-agnostic WASM row-mapper ABI + a Zig reference mapper (`peerdb/`). Mappers convert a row into the canonical string Seatbelt hashes. |
| `change-validation-core/` | The executable reference spec for Data Change Validation (Python). The Go program and DuckDB extension are ports of this logic. |
| `examples/` | Self-contained docker-compose examples: `sling-mysql-to-pg/`, `peerdb-pg-to-clickhouse/`. |
| `docs/` | Concept and setup guides for early adopters. |
| `_archive/` | Local-only (gitignored) copies of retired demos/experiments, kept for context. |

**Core Go packages (`seatbelt/pkg/`):**
- `postgres/` — PostgreSQL source: logical replication consumer + Go port of `hashtextextended`.
- `clickhouse/` — ClickHouse target with xxh3 hashing.
- `seatbelt/` — core validation + shadow/DuckDB triangulation logic.
- `row_mappers/` — WASM row-mapper host (wazero), embeds the reference `peerdb` mapper.
- `config/`, `typesystem/`, `csvutil/` — config loading, DB type registry, CSV helpers.

## Common Commands

### Go Application (`seatbelt/`)
```bash
go run cmd/seatbelt/main.go        # build and run
make build                         # build binary
make test                          # unit tests (also: go test ./...)
make wasm                          # rebuild + embed the WASM mapper
bash test/run_tests.sh             # integration tests with Docker
make up / make down                # start/stop test databases
make psql / make clickhouse-client # connect to test databases
```

### DuckDB Extension (`duckdb-seatbelt-extension/`)
```bash
make            # build extension
make test       # run SQL tests
# Built extension: ./build/release/extension/seatbelt_duckdb/seatbelt_duckdb.duckdb_extension
```

### WASM Mappers (`wasm-mappers/`)
```bash
cd peerdb && zig build      # -> peerdb/zig-out/bin/peerdb.wasm  (requires Zig 0.16+)
cd ../../seatbelt && make wasm   # copy the artifact into pkg/row_mappers/ for embedding
```

### Change-Validation Core spec (`change-validation-core/`)
```bash
python validation_logic.py   # runs the cross-language conformance test cases (needs colorama)
```

## Architecture Principles

### Hash-Based Validation (Hash Triangulation)
Source and destination hashes are **not** expected to match directly. Instead, a map of
`source_hash -> destination_hash` is computed asynchronously from the source change log, accounting
for pipeline transformations. The source side uses the fastest native hash (`hashtextextended`) for
minimal load; the destination side uses a deterministic hash. Mismatches against the map are
validation failures.

### Data Change Validation
Tracks INSERT/UPDATE/DELETE operations on source rows and asserts each produces an equivalent
operation on the destination — without inspecting row values. The canonical rules live in
`change-validation-core/validation_logic.py` and are re-implemented in Go and the DuckDB extension.

### Pluggable Row Mappers
Row mappers compile to WebAssembly and load at runtime, so third parties can ship mapping logic in
any language targeting `wasm32` without rebuilding Seatbelt. See `wasm-mappers/ABI.md`.

## Configuration (Go app)
Validation runs are configured via a `config.yaml` (see `seatbelt/examples/config.yaml` and the
`examples/` configs): `source_connection_string`, `target_connection_string`, `row_mapper_name`,
`table_name` / `target_table_name`, `primary_key_name`, a `columns` list (source/target types), and
optional `replication.slot_name` / `replication.publication_name`.

Environment variables: `SEATBELT_TEMP_DIR`, `SEATBELT_CLICKHOUSE_THREADS`.

## Testing Strategy
- **Go**: `go test ./...`; integration tests under `seatbelt/test/` (docker-compose Postgres + ClickHouse).
- **DuckDB**: `make test` (SQL tests under `test/sql/`).
- **Change-validation core**: `python validation_logic.py` (shared conformance cases in
  `validation_logic_tests.json`).
- **Examples**: each `examples/*/run.sh` brings up the stack and runs an end-to-end validation.

## Known Limitations
- Single-column primary keys only (compound PK support is a TODO in `pkg/seatbelt/table.go`).
