# CDC/ETL Tools Inventory

## By Subdirectory

### `seatbelt/` — Core Go Application
| Tool | Type | Evidence |
|------|------|----------|
| **PeerDB** | CDC (PG → ClickHouse) | Dedicated `PeerDBRowMapper` in `pkg/row_mappers/peer_db_row_mapper.go`; integration tests in `test/integration_peerdb_pg_to_ch_test.go`; example configs target a `peerdb` ClickHouse schema; `_peerdb_is_deleted` column filtering |
| **PostgreSQL Logical Replication** (native) | CDC source | Core of the seatbelt application — consumes `pgoutput` logical replication slots and publications directly |

### `demo-live/` — Live Replication Demo
| Tool | Type | Evidence |
|------|------|----------|
| **Sling** | ELT (MySQL → PostgreSQL) | `sling-conf/replication.yml` configures full-refresh MySQL → PostgreSQL sync; `Makefile` `sling_run` target; `sling` CLI invoked directly |
| **Debezium** | CDC connector (MySQL source) | Referenced in `README.md`; MySQL binlog CDC source connector for Kafka Connect (`--binlog-format=ROW` flags in docker-compose) |
| **Apache Kafka** | Message broker | Part of the Debezium → JDBC stack described in README |
| **Kafka Connect** | Integration framework | Hosts the Debezium MySQL source connector + JDBC PostgreSQL sink connector |
| **Zookeeper** | Kafka dependency | Listed as required component in README/docker stack |

### `exp-peerdb-duckdb/` — Experiment
| Tool | Type | Evidence |
|------|------|----------|
| **PeerDB** | CDC (PG → ClickHouse) | Data files named `seatbelt-clickhouse-scan-peerdb.*`; `validation.py` reads PeerDB-replicated ClickHouse output |

---

## Summary
- **PeerDB** — the primary production CDC pipeline, with first-class support (dedicated row mapper, integration tests, named configs)
- **Sling** — used in `demo-live` as an ELT tool for batch MySQL → PostgreSQL sync (full-refresh mode)
- **Debezium + Kafka + Kafka Connect** — used together in `demo-live` as an alternative CDC stack (MySQL binlog → Kafka → PostgreSQL via JDBC sink)
- **PostgreSQL Logical Replication** — native CDC mechanism that seatbelt's core reads directly (not a third-party tool per se, but the underlying CDC protocol)
