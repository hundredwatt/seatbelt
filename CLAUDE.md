# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a monorepo containing the Seatbelt data validation framework - a suite of tools for verifying data integrity between different database systems (PostgreSQL, ClickHouse, etc.) through hash-based validation techniques.

## Key Components

### 1. `seatbelt/` - Core Go Application
The main Seatbelt application written in Go that performs batch data validation by:
- Computing hashes via PostgreSQL SELECT queries using `hashtextextended`
- Consuming logical replication streams to compute matching hashes in Go
- Comparing results to validate data integrity

**Key packages:**
- `pkg/postgres/` - PostgreSQL integration, logical replication, and Go implementation of `hashtextextended`
- `pkg/clickhouse/` - ClickHouse integration with xxh3 hashing
- `pkg/config/` - Configuration management
- `pkg/seatbelt/` - Core data validation logic
- `pkg/row_mappers/` - Row mapping utilities (e.g., PeerDB integration)

### 2. `seatbelt-duckdb/` - DuckDB Extension
DuckDB extension that provides Seatbelt validation functions directly in SQL queries.

### 3. `pyseatbelt/` - Python Library
Python implementation of Seatbelt validation logic with abstract Source/Target interfaces.

### 4. `demo-tui/` - Terminal UI Demo
Interactive terminal application demonstrating Seatbelt's capabilities with:
- Customizable data schemas
- Corruption simulation
- Real-time validation feedback
- Testing framework

### 5. `demo-live/` - Live Replication Demo
Docker-based demonstration of MySQL to PostgreSQL replication using Debezium and Kafka.

## Common Commands

### Go Application (seatbelt/)
```bash
# Build and run
go run cmd/seatbelt/main.go

# Build binary
make build

# Run tests
make test
go test -v ./...

# Integration tests with Docker
bash test/run_tests.sh

# Development database
make up          # Start test databases
make down        # Stop test databases
make psql        # Connect to PostgreSQL
make clickhouse-client  # Connect to ClickHouse
```

### DuckDB Extension (seatbelt-duckdb/)
```bash
# Build extension
make

# Run tests
make test

# The built extension is at:
# ./build/release/extension/seatbelt_duckdb/seatbelt_duckdb.duckdb_extension
```

### Python Library (pyseatbelt/)
```bash
# Install in development mode
pip install -e .

# Run tests
python -m unittest discover tests
# or
python run_tests_with_logs.py
```

### Demo TUI (demo-tui/)
```bash
# Install dependencies
pip install -e .

# Run the TUI demo
python -m seatbelt_demo.ui.tui
# or
seatbelt-tui

# Run tests
./run_tests.py
```

## Architecture Principles

### Hash-Based Validation
The core concept involves computing identical hashes for the same data using different methods:
1. **Source**: PostgreSQL `hashtextextended` function on concatenated column values
2. **Target**: Go/Python implementation of the same hash function on replicated data
3. **Validation**: Compare hash results to detect data inconsistencies

### Logical Replication Integration
- Uses PostgreSQL logical replication slots and publications
- Processes INSERT/UPDATE/DELETE operations in real-time
- Maintains state consistency between source and target systems

### Pluggable Architecture
- Abstract Source/Target interfaces in Python
- Row mappers for different data pipeline integrations (PeerDB, etc.)
- Configurable column mappings and transformations

## Configuration

### Go Application
Uses `config.yaml` with structure:
```yaml
database:
  std_conn_string: "postgres://..."
  repl_conn_string: "postgres://..."
replication:
  slot_name: "seatbelt_slot"
  publication_name: "seatbelt_pub"
table:
  name: "schema.table"
  id_column: "id"
  hash_columns: ["col1", "col2"]
hash_seed: 42
output:
  select_csv_path: "output.csv"
  replication_csv_path: "replication.csv"
```

### Environment Variables
- `SEATBELT_TEMP_DIR` - Custom temporary directory location

## Testing Strategy

### Integration Tests
- Docker Compose setup in `test/` directories
- Automated database initialization with sample data
- Comparison of SELECT vs replication hash results
- Support for PostgreSQL and ClickHouse targets

### Unit Tests
- Go: `go test -v ./...`
- Python: `python -m unittest discover tests`
- DuckDB: `make test` (SQL-based tests)

### Demo Testing
- TUI includes interactive corruption scenarios
- YAML-based test configuration with expectations
- Metrics validation and error detection

## Development Workflow

1. **Database Setup**: Use `make up` to start test databases
2. **Code Changes**: Edit in respective language directories
3. **Testing**: Run appropriate test commands for each component
4. **Integration**: Use integration tests to verify cross-component compatibility
5. **Cleanup**: Use `make down` or `make clean` to reset environment