# Example: Postgres → ClickHouse via PeerDB

A self-contained example that replicates a Postgres table to ClickHouse with
[PeerDB](https://www.peerdb.io) (CDC) and validates the copy with Seatbelt's **Hash Triangulation**
using the `peer_db` WASM row mapper.

This is the flagship example: a **live CDC** pipeline with a JSON column — exactly the case where a
naive `MD5(json::text)` comparison between source and destination falls apart, and where Seatbelt's
mapper shines.

```
source-init/sample-data.csv ─► Postgres (public.events) ─PeerDB CDC─► ClickHouse (default.events)
                                                                              │
                                                          Seatbelt validates ◄┘
                                                          (peer_db WASM mapper)
```

## What's here

| Path | Purpose |
|------|---------|
| `docker-compose.yml` | A trimmed PeerDB control plane + source Postgres (`:55831`) + ClickHouse (`:9100`) |
| `peerdb-config/` | Config vendored from upstream PeerDB (catalog `postgresql.conf`, Temporal dynamic config, replication `pg_hba` hook) |
| `source-init/` | `events` schema + `sample-data.csv` (10 rows, including a JSONB `config` column) |
| `setup-peerdb.sql` | Creates the source/target peers and the CDC mirror |
| `config.yaml` | Seatbelt validation config (`row_mapper_name: peer_db`) |
| `run.sh` / `teardown.sh` | Run the whole example / clean up |

## Prerequisites

- Docker + Docker Compose (the PeerDB stack is ~9 containers; first run pulls several images)
- Go 1.25+ (to build the `seatbelt` binary)

## Run it

```bash
./run.sh
```

`run.sh` starts the stack, creates the peers + mirror, waits for the snapshot to land in ClickHouse,
builds Seatbelt, and validates. Allow a few minutes the first time.

### Expected output

Once the 10 rows have replicated:

```
--- Validation Metrics ---
Metric                                 Count
---------------------------------------------
Source Row Count                          10
Target Row Count                          10
Valid Rows                                10
Pending Rows                               0
Error Rows                                 0
---------------------------------------------
```

All 10 rows reconcile — **including** the rows whose `config` JSON has its keys in a different order
in different rows. PeerDB stores the JSON as a ClickHouse `String`; the `peer_db` mapper reproduces
PeerDB's canonical form on the source side so the hashes line up. A byte-for-byte `MD5(json::text)`
comparison would report false mismatches on exactly these rows.

### Verified at scale

The same `peer_db` mapper and validation path was run against a real PeerDB pipeline replicating a
**3.2 M-row, 4 GB** Postgres table (with `jsonb`) to ClickHouse:

```
Target scan completed in 1.9s, 3,218,936 rows (2.6% transfer efficiency)
Source Row Count   3,218,936
Target Row Count   3,218,936
Valid Rows         3,218,935
Pending Rows               1     <- one row changed mid-scan; reconciles on the next pass
Error Rows                 0
```

The ClickHouse side read only **2.6%** of the table's size — it pulls one hash per row, not the rows
themselves.

## See it catch a problem

Tamper with the ClickHouse copy and re-validate:

```bash
docker exec seatbelt_peerdb_clickhouse clickhouse-client --user default --password pass \
  -q "INSERT INTO default.events (id, name, _peerdb_is_deleted, _peerdb_version) VALUES (999, 'ghost', 0, 1)"

rm -f tmp/shadow.db
./bin/seatbelt run -c config.yaml --initial-load
```

```
Source Row Count                          10
Target Row Count                          11     <- the phantom row
Valid Rows                                11
...
```

The phantom row (`id=999`) exists in the destination but not the source, so the **destination count
exceeds the source count** — the mismatch is the signal. In batch (`--initial-load`) mode every row
is treated as an insert, so the extra row isn't yet promoted to an Error; on a live pipeline the Data
Change Validation rules flag it as a phantom (see below).

> **Batch vs. live:** This example validates with `--initial-load` for simplicity. On a continuously
> running pipeline, Seatbelt instead consumes the Postgres change log so the Data Change Validation
> rules can distinguish replication lag from genuine errors. See
> [`../../docs/04-setup-peerdb.md`](../../docs/04-setup-peerdb.md) and
> [`../../docs/01-concepts.md`](../../docs/01-concepts.md).

## Writing your own mapper

The `peer_db` mapper is a WASM module (Zig reference implementation in
[`../../wasm-mappers/peerdb`](../../wasm-mappers/peerdb)). To validate a pipeline other than PeerDB,
implement the mapper ABI ([`../../wasm-mappers/ABI.md`](../../wasm-mappers/ABI.md)) and load your
`.wasm` — no need to rebuild Seatbelt.

## Clean up

```bash
./teardown.sh
```
