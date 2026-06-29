# Example: MySQL → Postgres via Sling

A fully self-contained example that replicates a MySQL table to Postgres with
[Sling](https://slingdata.io) (full-refresh) and validates the copy with Seatbelt.

This is the simplest example — a **batch** pipeline, so Seatbelt runs in `--initial-load` mode
(no change stream). It exercises Seatbelt's MySQL source, Postgres target, and the `generic` identity
row mapper.

```
seed/products.csv ──► MySQL (shop.products) ──Sling──► Postgres (public.products)
                                                              │
                                                  Seatbelt validates ◄──┘
```

## What's here

| File | Purpose |
|------|---------|
| `docker-compose.yml` | MySQL 8 (`:55820`) + Postgres 16 (`:55821`) + a one-shot Sling job |
| `seed/products.csv` | Sample data (10 product rows) — the source of truth |
| `mysql-init/01-schema.sql` | Creates `shop.products` in MySQL |
| `sling/replication.yml` | Sling full-refresh config (MySQL → Postgres) |
| `config.yaml` | Seatbelt validation config (`row_mapper_name: generic`) |
| `run.sh` / `teardown.sh` | Run the whole example / clean up |

## Prerequisites

- Docker + Docker Compose
- Go 1.25+ (to build the `seatbelt` binary)

## Run it

```bash
./run.sh
```

`run.sh` brings up the databases, loads the CSV into MySQL, runs Sling, builds Seatbelt, and
validates.

### Expected output

```
==> [3/4] Replicating to Postgres with Sling
... inserted 10 rows into "public"."products" ...
... execution succeeded

==> [4/4] Validating with Seatbelt (initial-load)
Target scan completed in 880µs, 10 rows (... 1.5% transfer efficiency)
Source extract scan completed in 695µs, 10 rows (...)
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

All 10 rows reconcile: source and destination counts match and every row is **Valid**.

## See it catch a problem

Tamper with the destination, then re-validate:

```bash
docker exec -i seatbelt_sling_postgres psql -U postgres -d shop \
  -c "UPDATE public.products SET quantity = 9999 WHERE id = 3; DELETE FROM public.products WHERE id = 7;"

rm -f tmp/shadow.db
./bin/seatbelt run -c config.yaml --initial-load
```

```
--- Validation Metrics ---
Source Row Count                          10
Target Row Count                           9      <- the deleted row is missing
Valid Rows                                 8
Pending Rows                               2      <- corrupted + missing rows don't reconcile
Error Rows                                 0
---------------------------------------------
```

The corrupted row (`id=3`) and the deleted row (`id=7`) no longer reconcile, so they drop out of
**Valid** into **Pending**, and the destination row count falls to 9.

> **Batch vs. live:** In `--initial-load` (batch) mode, unreconciled rows are reported as **Pending** —
> Seatbelt can't yet tell a permanent error from in-flight replication lag without a second
> observation. On a *live* pipeline, Seatbelt consumes the source change log and the Data Change
> Validation rules promote genuine discrepancies to **Error**. See
> [`../peerdb-pg-to-clickhouse`](../peerdb-pg-to-clickhouse) for the live path, and
> [`../../docs/01-concepts.md`](../../docs/01-concepts.md) for why.

Restore the clean state by re-running Sling: `docker compose run --rm sling`.

## How the validation works

- **Source (MySQL)** and **target (Postgres)** are selected automatically from the `mysql://` and
  `postgres://` schemes in `config.yaml`.
- The `generic` row mapper canonicalizes each row as the concatenation of its column values (a ghost
  sentinel for NULLs), matching the `COALESCE(col::text, …)` expression Seatbelt builds on the
  Postgres side. This works because Sling copies these integer/string columns without changing their
  text form.
- Columns whose text representation differs across engines (JSON key order, float precision, decimals,
  timestamps) need a purpose-built mapper — see [`../../wasm-mappers`](../../wasm-mappers).

## Clean up

```bash
./teardown.sh
```
