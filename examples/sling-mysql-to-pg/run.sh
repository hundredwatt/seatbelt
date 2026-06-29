#!/usr/bin/env bash
# End-to-end MySQL -> Postgres (Sling) validation with Seatbelt.
#
#   1. Bring up MySQL + Postgres (docker compose)
#   2. Load seed/products.csv into MySQL
#   3. Replicate to Postgres with Sling (full-refresh)
#   4. Validate the copy with Seatbelt (initial-load / batch mode)
#
# Re-run safe. Use ./teardown.sh to remove containers.
set -euo pipefail
cd "$(dirname "$0")"

REPO_ROOT="$(cd ../.. && pwd)"
SEATBELT_BIN="./bin/seatbelt"

echo "==> [1/4] Starting MySQL + Postgres"
docker compose up -d mysql postgres

echo "==> Waiting for databases to be healthy"
for svc in mysql postgres; do
  until [ "$(docker inspect -f '{{.State.Health.Status}}' "seatbelt_sling_${svc}" 2>/dev/null || echo starting)" = "healthy" ]; do
    sleep 2
  done
  echo "    ${svc} healthy"
done

echo "==> [2/4] Loading seed/products.csv into MySQL"
docker exec -i seatbelt_sling_mysql mysql -uroot -prootpw shop -e "TRUNCATE TABLE products;"
docker exec -i seatbelt_sling_mysql mysql --local-infile=1 -uroot -prootpw shop -e \
  "LOAD DATA LOCAL INFILE '/dev/stdin' INTO TABLE products \
   FIELDS TERMINATED BY ',' OPTIONALLY ENCLOSED BY '\"' \
   LINES TERMINATED BY '\n' IGNORE 1 LINES \
   (id, sku, name, category, quantity);" < seed/products.csv
echo "    loaded $(($(wc -l < seed/products.csv) - 1)) rows"

echo "==> [3/4] Replicating to Postgres with Sling"
docker compose run --rm sling

echo "==> [4/4] Validating with Seatbelt (initial-load)"
mkdir -p tmp
rm -f tmp/shadow.db
echo "    building seatbelt binary"
( cd "$REPO_ROOT/seatbelt" && go build -o "$REPO_ROOT/examples/sling-mysql-to-pg/bin/seatbelt" ./cmd/seatbelt )
"$SEATBELT_BIN" run -c config.yaml --initial-load

echo ""
echo "Done. Tear down with: ./teardown.sh"
