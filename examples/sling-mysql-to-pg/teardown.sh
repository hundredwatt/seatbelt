#!/usr/bin/env bash
# Stop and remove the example's containers, volumes, and local scratch files.
set -euo pipefail
cd "$(dirname "$0")"
docker compose down -v --remove-orphans
rm -rf tmp bin
echo "Torn down."
