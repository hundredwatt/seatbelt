#!/bin/bash
# Loads sample-data.csv into the events table. Runs automatically on first container start.
set -e
psql -v ON_ERROR_STOP=1 -U postgres -d source -c \
  "COPY events (id, user_id, name, email, status, created_at, score, description, config) \
   FROM '/docker-entrypoint-initdb.d/sample-data.csv' WITH (FORMAT csv, HEADER true);"
