#!/usr/bin/env bash
# End-to-end PeerDB Postgres -> ClickHouse validation with Seatbelt.
#
#   1. Bring up the PeerDB control plane + source Postgres + ClickHouse
#   2. Create the source/target peers and the CDC mirror
#   3. Wait for PeerDB to snapshot the rows into ClickHouse
#   4. Validate source vs destination with Seatbelt (peer_db WASM mapper)
#
# Heads up: the PeerDB stack is ~9 containers and pulls several images on first run. Bringing it up
# and snapshotting takes a few minutes. Use ./teardown.sh to remove everything.
set -euo pipefail
cd "$(dirname "$0")"

REPO_ROOT="$(cd ../.. && pwd)"
SEATBELT_BIN="./bin/seatbelt"

echo "==> [1/4] Starting PeerDB + Postgres + ClickHouse (this can take a few minutes)"
docker compose up -d

echo "==> Waiting for source Postgres, ClickHouse, and the PeerDB server"
until docker exec seatbelt_peerdb_source pg_isready -U postgres >/dev/null 2>&1; do sleep 2; done
until docker exec seatbelt_peerdb_clickhouse wget -q --spider http://localhost:8123/ping >/dev/null 2>&1; do sleep 2; done
until pg_isready -h localhost -p 9900 >/dev/null 2>&1; do sleep 2; done
echo "    services up"

echo "==> [2/4] Creating PeerDB peers + mirror"
# The PeerDB server speaks the Postgres wire protocol on :9900 (default password: peerdb).
PGPASSWORD=peerdb psql "postgres://postgres@localhost:9900" -v ON_ERROR_STOP=1 -f setup-peerdb.sql

echo "==> [3/4] Waiting for the initial snapshot to land in ClickHouse"
EXPECTED=$(( $(wc -l < source-init/sample-data.csv) - 1 ))
for _ in $(seq 1 60); do
  COUNT=$(docker exec seatbelt_peerdb_clickhouse clickhouse-client --user default --password pass \
    -q "SELECT count() FROM default.events" 2>/dev/null || echo 0)
  echo "    clickhouse rows: ${COUNT}/${EXPECTED}"
  [ "${COUNT}" -ge "${EXPECTED}" ] && break
  sleep 5
done

echo "==> [4/4] Validating with Seatbelt"
mkdir -p tmp
rm -f tmp/shadow.db
( cd "$REPO_ROOT/seatbelt" && go build -o "$REPO_ROOT/examples/peerdb-pg-to-clickhouse/bin/seatbelt" ./cmd/seatbelt )
"$SEATBELT_BIN" run -c config.yaml --initial-load

echo ""
echo "Done. Tear down with: ./teardown.sh"
