#!/usr/bin/env bash
# Derive dataset1gb and dataset10gb from an existing large table (default: dataset100gb) in the local
# Postgres, sized by row count (~933 bytes/row in the reference schema). Idempotent.
set -euo pipefail

PGURL="${PGURL:-postgres://localhost:5432/jason}"
SRC="${SRC_TABLE:-dataset100gb}"

make_subset() {
  local name="$1" rows="$2"
  echo "==> Building ${name} (${rows} rows) from ${SRC}"
  psql "$PGURL" -v ON_ERROR_STOP=1 <<SQL
DROP TABLE IF EXISTS ${name};
CREATE TABLE ${name} AS
  SELECT * FROM ${SRC} ORDER BY id LIMIT ${rows};
ALTER TABLE ${name} ADD PRIMARY KEY (id);
SQL
  psql "$PGURL" -At -c \
    "SELECT '    ${name}: ' || count(*) || ' rows, ' || pg_size_pretty(pg_total_relation_size('${name}')) FROM ${name};"
}

make_subset dataset1gb  1150000
make_subset dataset10gb 11500000

echo "Done."
