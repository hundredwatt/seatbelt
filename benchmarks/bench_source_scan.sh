#!/usr/bin/env bash
# Benchmark Seatbelt's source scan — the operation that determines source-side impact. It runs the
# real `seatbelt benchmark source-scan`, which executes a single in-database COPY that emits one hash
# per row, then reports wall time and bytes transferred from the source (absolute and as a % of the
# table's on-disk size).
#
# Usage: ./bench_source_scan.sh <table_name>
set -euo pipefail
cd "$(dirname "$0")"

TABLE="${1:?usage: bench_source_scan.sh <table_name>}"
PGURL="${PGURL:-postgres://localhost:5432/jason}"
REPO_ROOT="$(cd .. && pwd)"
SEATBELT_BIN="./bin/seatbelt"
export SEATBELT_TEMP_DIR="./tmp"

mkdir -p tmp
[ -x "$SEATBELT_BIN" ] || ( cd "$REPO_ROOT/seatbelt" && go build -o "$REPO_ROOT/benchmarks/bin/seatbelt" ./cmd/seatbelt )

# Generate a config for this table (peer_db mapper; target conn is a placeholder — source-scan never
# connects to it).
CFG="tmp/${TABLE}.yaml"
cat > "$CFG" <<YAML
source_connection_string: "${PGURL}"
target_connection_string: "clickhouse://default:pass@localhost:9000/peerdb?password=pass"
row_mapper_name: "peer_db"
table_name: "public.${TABLE}"
target_table_name: "peerdb.public_${TABLE}"
primary_key_name: "id"
columns:
  - {name: user_id, source_type: integer, target_type: Int32}
  - {name: name, source_type: character varying, target_type: String}
  - {name: email, source_type: character varying, target_type: String}
  - {name: status, source_type: character varying, target_type: String}
  - {name: created_at, source_type: timestamp without time zone, target_type: DateTime64(6)}
  - {name: score, source_type: numeric, target_type: Decimal(10,2)}
  - {name: description, source_type: text, target_type: String}
environment:
  SEATBELT_TEMP_DIR: "./tmp/"
YAML

rm -f tmp/seatbelt-scan-*.csv
TABLE_BYTES=$(psql "$PGURL" -At -c "SELECT pg_total_relation_size('${TABLE}');")

START=$(date +%s.%N)
"$SEATBELT_BIN" benchmark source-scan -c "$CFG" >/dev/null 2>tmp/bench.err
END=$(date +%s.%N)

SCAN_FILE=$(ls -t tmp/seatbelt-scan-*.csv 2>/dev/null | head -1)
SCAN_BYTES=$(stat -f%z "$SCAN_FILE" 2>/dev/null || stat -c%s "$SCAN_FILE")
WALL=$(echo "$END - $START" | bc)
PCT=$(echo "scale=4; ($SCAN_BYTES / $TABLE_BYTES) * 100" | bc)

printf "%-14s table=%s  transferred=%s  (%.3f%% of table)  wall=%.1fs\n" \
  "$TABLE" \
  "$(numfmt --to=iec "$TABLE_BYTES" 2>/dev/null || echo "${TABLE_BYTES}B")" \
  "$(numfmt --to=iec "$SCAN_BYTES" 2>/dev/null || echo "${SCAN_BYTES}B")" \
  "$PCT" "$WALL"
