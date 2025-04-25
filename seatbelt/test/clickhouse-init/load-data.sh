#!/bin/bash

# Wait for ClickHouse to be ready
until clickhouse-client --host localhost --user default --password "" --query "SELECT 1";
do
  echo "Waiting for ClickHouse to start..."
  sleep 1
done

# Load data from binary file
clickhouse-client --host localhost --user default --password "" --query "INSERT INTO peerdb.public_data_proof FORMAT Native" < /docker-entrypoint-initdb.d/peerdb_public_data_proof.clickhouse.bin

echo "Data loaded successfully" 