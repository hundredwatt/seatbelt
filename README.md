# Seatbelt

**End-to-end validation for live, high-throughput CDC pipelines.**

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

Seatbelt verifies that a change-data-capture pipeline (Postgres → ClickHouse via PeerDB,
MySQL → Postgres via Sling, etc.) is actually moving your data correctly — across *all* rows and
columns, while the source keeps changing, without hammering the source database.

It does this with two primitives:

- **Data Change Validation** — detects a broad class of pipeline failures (missing rows, missing
  updates, extra rows, duplicates) by tracking row-level *operations* over time. No per-table or
  per-column configuration.
- **Hash Triangulation** — a full source-to-destination comparison of every row and column, computed
  asynchronously from the source's change log so it tolerates in-flight changes and pipeline
  transformations, with minimal load on the source.

> Seatbelt didn't make it as a commercial product. It's open source (MIT) so the techniques don't die
> with the company. If you run data pipelines, the ideas here are worth stealing.

---

## The problem: you can't just hash both sides and compare

The obvious way to test a pipeline is to hash each source row, hash the corresponding destination row,
and compare. In practice this almost never works on a live pipeline, even when replication is
perfectly correct:

- **JSON columns** — `MD5(json::text)` differs between source and destination because JSON
  serialization doesn't guarantee key ordering.
- **Lossy type conversions** — floating-point truncation, timestamp precision, numeric widening, etc.
- **Live changes** — the source row changed and the change hasn't propagated yet, so a point-in-time
  comparison sees a "mismatch" that isn't one.

Seatbelt started from a different question: *assume you can never get a clean row-for-row comparison
between source and destination — can you still comprehensively test a live pipeline?*

## Data Change Validation

Instead of comparing values, watch how data **moves**. Every INSERT/UPDATE/DELETE on a source row
should produce an equivalent operation on a destination row — regardless of what the values are or how
they were transformed. From that single invariant, Seatbelt asserts:

At the **table** level:
- No source rows missing from the destination
- No destination rows missing from the source
- Row counts match (after accounting for replication lag)

At the **row** level:
- Every DML operation on a source row produces an equivalent operation on the destination

It needs **zero** per-table or per-column setup. The operation model and the exact failure-detection
rules live in [`change-validation-core/`](./change-validation-core) as an executable reference spec
that the Go program and the DuckDB extension both implement.

## Hash Triangulation

To answer "does the source match the destination *exactly*?" Seatbelt avoids the broken approach of
expecting `source_hash == destination_hash`. Instead it builds a map of
`source_hash → destination_hash` for every row, computed **asynchronously from the source change log**
(where the pipeline's transformations can be reproduced once, in one place). Then:

1. Use Data Change Validation to isolate the *static* rows (filtering out live churn and anything
   already flagged).
2. For each row, compute a cheap **source hash** with the fastest native function on the source
   (`hashtextextended`, minimal load) and a deterministic **destination hash**.
3. From the change log, maintain the `source_hash → destination_hash` map.
4. Compare the observed `(source_hash, destination_hash)` pairs against the map. A mismatch is a
   validation failure.

This gives a full all-rows/all-columns audit with minimal source I/O, at the cost of writing a
**row mapper** that reproduces how the pipeline transforms rows. Mappers compile to WebAssembly so
you can write them in any language — see [`wasm-mappers/`](./wasm-mappers).

## Repository layout

| Path | What it is |
|------|------------|
| [`seatbelt/`](./seatbelt) | Core Go program. Source hashing via Postgres `hashtextextended`, a logical-replication consumer, and DuckDB-based triangulation. |
| [`duckdb-seatbelt-extension/`](./duckdb-seatbelt-extension) | DuckDB extension exposing the data-change-validation functions as SQL. |
| [`wasm-mappers/`](./wasm-mappers) | Language-agnostic WASM row-mapper ABI + a Zig reference mapper for PeerDB → ClickHouse. |
| [`change-validation-core/`](./change-validation-core) | Executable reference spec (Python) for the validation logic, with a cross-language conformance suite. |
| [`examples/`](./examples) | Self-contained, `docker compose`-based examples (see below). |
| [`docs/`](./docs) | Concept and setup guides. |
| [`benchmarks/`](./benchmarks) | Source-impact + throughput benchmarks (with a reproducible harness). |

## Quick start

Start with the simplest example — MySQL → Postgres via Sling — which runs entirely in Docker:

```bash
cd examples/sling-mysql-to-pg
./run.sh
```

Then try the Postgres → ClickHouse via PeerDB example, which exercises Hash Triangulation through the
WASM row mapper:

```bash
cd examples/peerdb-pg-to-clickhouse
./run.sh
```

Each example's `README.md` has the full walkthrough and the expected output.

## Benchmarks

Seatbelt reads only **one hash per row** from the source — never the row data — so validating a table
costs a small, constant fraction of its size:

| Table | Size | Transferred from source | % of table | Wall time |
|-------|------|-------------------------|-----------|-----------|
| 1 GB   | 1.0 GB | 33 MB  | **3.14 %** | 1 s |
| 10 GB  | 10 GB  | 323 MB | **3.14 %** | 8 s |
| 100 GB | 103 GB | 3.3 GB | **3.19 %** | 247 s |

A real PeerDB Postgres → ClickHouse pipeline (4 GB, 3.2 M rows, including a JSON column) validated
end-to-end with the destination scan reading just **2.6 %** of the table's size. Full methodology and a
reproducible harness in [`benchmarks/`](./benchmarks).

## Documentation

- [Concepts](./docs/01-concepts.md) — Data Change Validation & Hash Triangulation in depth
- [Architecture](./docs/02-architecture.md) — how the Go tool actually works
- [Setup with Sling](./docs/03-setup-sling.md) — MySQL → Postgres walkthrough
- [Setup with PeerDB](./docs/04-setup-peerdb.md) — Postgres → ClickHouse, incl. writing a custom mapper

## Known limitations

This is a research-grade tool shared for its ideas, not a turnkey product:

- **Single-column integer primary keys** only (compound-PK support is a TODO).
- First-class pipelines are **PeerDB (PG → ClickHouse)** and **Sling (MySQL → PG)**; other pipelines
  need a row mapper.
- Operational concerns (HA, retries, packaging) are intentionally minimal.

## Background

There's a longer write-up of the motivation and a relevant
[r/dataengineering discussion](https://www.reddit.com/r/dataengineering/comments/1e9txka/data_pipeline_testing_howwhat_do_you_guys_do/)
on how teams test data pipelines today.

## License

MIT — see [LICENSE](./LICENSE).
