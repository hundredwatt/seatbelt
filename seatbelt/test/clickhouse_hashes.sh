#!/bin/bash
set -e

clickhouse-client --password pass -d peerdb --query "
    SELECT 
      id, 
      concat(CAST(smallint_col AS String), CAST(bigint_col AS String), CAST(float_col AS String), CAST(double_col AS String)) as row_string,
      xxh3(concat(CAST(smallint_col AS String), CAST(bigint_col AS String), CAST(float_col AS String), CAST(double_col AS String))) AS computed_hash
    FROM public_data_proof;
  " --format csv > testdata/clickhouse_data_proof_hashes.csv
