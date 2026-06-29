# Benchmarks

Seatbelt's pitch is a *full* audit with *minimal* source impact. These benchmarks quantify the second
half: how much data Seatbelt pulls from the source to validate a table, and how fast.

## What's measured

For a validation, the source-side cost is a single in-database `COPY` that emits **one hash per row**
(`pk, hashtextextended(...)`). Seatbelt never extracts the row data itself. We measure:

1. **Wall time** of the source scan (the real `seatbelt benchmark source-scan`).
2. **Bytes transferred from the source** = size of the hash output, reported absolutely and as a **% of
   the table's on-disk size** (`pg_total_relation_size`).

Postgres here is local, so we report *logical* bytes (the hash stream), which is the meaningful,
reproducible figure — wire bytes would just be this minus TCP overhead. The percentage is what
matters: it's the fraction of your table Seatbelt has to read to validate it.

## Reproduce

```bash
# 1. Derive 1GB and 10GB tables from an existing large table (default: dataset100gb)
./create_datasets.sh

# 2. Benchmark the source scan
./bench_source_scan.sh dataset1gb
./bench_source_scan.sh dataset10gb
```

Requires a local Postgres with the reference schema (`id, user_id, name, email, status, created_at,
score, description`) and Go 1.25+. Override the connection with `PGURL=...`.

## Results

Measured on the reference schema (~933 bytes/row), local PostgreSQL 17, Apple Silicon Mac mini.

### Source impact (source scan)

| Table | Size | Rows | Transferred from source | % of table | Wall time |
|-------|------|------|-------------------------|-----------|-----------|
| `dataset1gb`  | 1.0 GB | 1.15 M | 33 MB  | **3.14 %** | 3.0 s |
| `dataset10gb` | 10 GB  | 11.5 M | 323 MB | **3.14 %** | 8.0 s |

To validate a table, Seatbelt reads ~**3 %** of its size from the source — a constant ratio (one
fixed-width hash per row), so it scales linearly and predictably. A 10× larger table costs ~10× the
(small) bytes, not a full re-read.

### Full pipeline (Postgres → ClickHouse via PeerDB)

A real PeerDB pipeline replicating a Postgres table (with a `jsonb` column) to ClickHouse, validated
end-to-end with `seatbelt run --initial-load`:

| Table | Size | Rows | Result | Destination scan | Destination transfer |
|-------|------|------|--------|------------------|---------------------|
| `demo_3m` | 4.0 GB | 3.22 M | 3,218,935 Valid / 1 Pending / 0 Error | 1.9 s | **2.6 % of table** |

The ClickHouse side likewise pulled ~2.6% of the table's size — one hash per row — and every row
(including the JSON column that a naive `MD5(json::text)` comparison would flag) reconciled.

## Notes

- The 1GB/10GB rows above use the real `seatbelt benchmark source-scan`; the `demo_3m` row is a real
  `seatbelt run` against an actual PeerDB-replicated ClickHouse table on the development machine.
- A live `seatbelt run` (without `--initial-load`) additionally consumes the change log; that cost is
  bounded by the pipeline's change rate, not the table size.
