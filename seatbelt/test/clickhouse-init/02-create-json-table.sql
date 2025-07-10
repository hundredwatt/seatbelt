USE peerdb;

CREATE TABLE IF NOT EXISTS public_json_example (
    id UInt64,
    json_data String,
    _peerdb_synced_at DateTime64(9) DEFAULT now64(),
    _peerdb_is_deleted Int8,
    _peerdb_version Int64
) ENGINE = ReplacingMergeTree()
ORDER BY id;

INSERT INTO public_json_example (id, json_data, _peerdb_synced_at, _peerdb_is_deleted, _peerdb_version) VALUES (1, '{"age":30,"name":"John"}', now64(), 0, 1);
INSERT INTO public_json_example (id, json_data, _peerdb_synced_at, _peerdb_is_deleted, _peerdb_version) VALUES (2, '{"age":25,"name":"Jane"}', now64(), 0, 1);
INSERT INTO public_json_example (id, json_data, _peerdb_synced_at, _peerdb_is_deleted, _peerdb_version) VALUES (3, '{"age":35,"name":"Jim"}', now64(), 0, 1);