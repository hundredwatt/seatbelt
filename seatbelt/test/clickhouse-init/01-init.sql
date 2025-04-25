CREATE DATABASE IF NOT EXISTS peerdb;

USE peerdb;

CREATE TABLE IF NOT EXISTS public_data_proof
(
    id Int32,
    smallint_col Int16,
    bigint_col Int64,
    float_col Float32,
    double_col Float64,
    _peerdb_synced_at DateTime64(9) DEFAULT now64(),
    _peerdb_is_deleted Int8,
    _peerdb_version Int64
) ENGINE = ReplacingMergeTree()
ORDER BY id; 