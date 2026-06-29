# Setup: validating a Sling MySQL → Postgres pipeline

This walks through validating a batch (full-refresh) [Sling](https://slingdata.io) pipeline that
copies a MySQL table to Postgres. The runnable version is
[`../examples/sling-mysql-to-pg`](../examples/sling-mysql-to-pg) — start there if you just want to see
it work; this guide explains how to point Seatbelt at *your* Sling pipeline.

## When to use this

- Your pipeline is **batch** (full-refresh or truncate-reload), not streaming CDC.
- The columns you care about are copied without changing their text form — integers, strings, and
  other types whose `::text` representation matches across MySQL and Postgres.

Because there's no change stream, Seatbelt runs in `--initial-load` mode and reports unreconciled rows
as **Pending** (see [`01-concepts.md`](./01-concepts.md)).

## 1. Build the binary

```bash
cd seatbelt && go build -o bin/seatbelt ./cmd/seatbelt
```

## 2. Write the config

Seatbelt picks the source/target implementations from the connection-string schemes
(`mysql://` → MySQL source, `postgres://` → Postgres target):

```yaml
source_connection_string: "mysql://user:pass@mysql-host:3306/shop"
target_connection_string: "postgres://postgres:postgres@pg-host:5432/shop"

row_mapper_name: "generic"        # identity mapper for 1:1 copies

table_name: "products"            # MySQL table (database comes from the connection string)
target_table_name: "public.products"   # where Sling wrote it
primary_key_name: "id"            # single integer PK

columns:                          # only the columns you want to validate
  - {name: sku,      source_type: varchar, target_type: text}
  - {name: name,     source_type: varchar, target_type: text}
  - {name: quantity, source_type: int,     target_type: integer}

seatbelt_data_path: "tmp/shadow.db"
environment:
  SEATBELT_TEMP_DIR: "tmp/"
```

The `generic` mapper canonicalizes each row as the concatenation of its column values (a ghost
sentinel for NULLs), matching the `COALESCE(col::text, …)` expression Seatbelt builds on the Postgres
side.

## 3. Run the validation

After Sling finishes a refresh:

```bash
./bin/seatbelt run -c config.yaml --initial-load
```

```
--- Validation Metrics ---
Source Row Count                          10
Target Row Count                          10
Valid Rows                                10
Pending Rows                               0
Error Rows                                 0
```

Matching counts with everything **Valid** means the refresh landed cleanly. A drop in Valid (rows
moving to **Pending**) or a count mismatch points at missing or corrupted rows — re-run after the next
refresh to confirm it isn't just timing.

## Caveats

- **Type representations must match.** JSON (key ordering), floating-point, decimal scale, and
  timestamp precision often differ between MySQL and Postgres. For those columns the `generic` mapper
  isn't enough — write a purpose-built mapper ([`../wasm-mappers`](../wasm-mappers)) that reproduces
  the destination's canonical form, or exclude the column from `columns`.
- **Single integer primary key** only.
- Re-running deletes and rebuilds the shadow DB (`rm -f tmp/shadow.db`) for a clean batch comparison.
