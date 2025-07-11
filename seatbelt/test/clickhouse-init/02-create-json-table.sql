USE peerdb;

CREATE TABLE IF NOT EXISTS public_json_example (
    id UInt64,
    json_data String,
    int_value Int64,
    _peerdb_synced_at DateTime64(9) DEFAULT now64(),
    _peerdb_is_deleted Int8,
    _peerdb_version Int64
) ENGINE = ReplacingMergeTree()
ORDER BY id;

INSERT INTO public_json_example (id, json_data, int_value, _peerdb_synced_at, _peerdb_is_deleted, _peerdb_version) VALUES (1, '{"age":30,"name":"John"}', 1, now64(), 0, 1);
INSERT INTO public_json_example (id, json_data, int_value, _peerdb_synced_at, _peerdb_is_deleted, _peerdb_version) VALUES (2, '{"age":25,"name":"Jane"}', 2, now64(), 0, 1);
INSERT INTO public_json_example (id, json_data, int_value, _peerdb_synced_at, _peerdb_is_deleted, _peerdb_version) VALUES (3, '{"age":35,"name":"Jim"}', 3, now64(), 0, 1);