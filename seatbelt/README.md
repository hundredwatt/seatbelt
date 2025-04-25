# Seatbelt Source Postgres Batch Processor

**Target Audience:** LLMs, Future Developers

## Repository Purpose

This repository contains a Go application designed to perform batch processing on a PostgreSQL database table. Its primary function is to verify the consistency of hash values generated in two ways:

1.  **SELECT Query:** Directly computing a hash within PostgreSQL using the `hashtextextended` function on concatenated text representations of specified columns.
2.  **Logical Replication:** Consuming logical replication events (INSERTs, UPDATEs) for the same table, reconstructing the same text representation of the specified columns from the replication stream (using text format), and computing the hash using a Go implementation (`pkg/postgres_funcs/hashtextextend.go`) that mirrors the PostgreSQL function.

The goal is to ensure that the Go implementation of the hash function produces identical results to the native PostgreSQL function for the same row data state.

## Execution Flow (Batch Job)

The main application (`cmd/seatbelt/main.go`) runs as a single batch job:

1.  **Load Configuration:** Reads settings from `config.yaml` (database connections, replication slot/publication, target table, columns to hash, hash seed, output paths).
2.  **Connect to DB:** Establishes a standard connection pool to PostgreSQL.
3.  **Run SELECT Query:**
    *   Constructs a SQL query dynamically based on the configuration.
    *   Selects the `id` and computes the `hashtextextended` hash for each row in the configured table.
    *   Stores the results (`id -> hash`) in memory.
4.  **Write SELECT CSV:** Writes the results from the SELECT query to the CSV file specified in `config.yaml` (`output.select_csv_path`).
5.  **Connect for Replication:** Establishes a logical replication connection.
6.  **Consume Replication Stream:**
    *   Starts consuming messages from the configured replication slot and publication.
    *   Processes `INSERT` and `UPDATE` messages for the configured table.
    *   For each relevant message, it extracts the necessary columns (in text format).
    *   Concatenates the text values of the configured `hash_columns`.
    *   Computes the hash using `pkg/postgres_funcs/PostgresHashtextextend`.
    *   Stores the result (`id -> hash`) in an in-memory map, overwriting previous entries for the same ID (capturing the latest state).
    *   Continues until the replication stream is idle for the configured `idle_timeout` duration or until a shutdown signal (SIGINT/SIGTERM) is received.
7.  **Write Replication CSV:** Writes the final `id -> hash` map collected from the replication stream to the CSV file specified in `config.yaml` (`output.replication_csv_path`). This file represents the state captured via replication.

## Configuration (`config.yaml`)

The application behavior is controlled by `config.yaml`:

*   `database`: Connection strings for standard (`std_conn_string`) and replication (`repl_conn_string`) connections.
*   `replication`: `slot_name`, `publication_name`, and `idle_timeout` for the replication consumer.
*   `table`: `name` (schema-qualified), `id_column`, and a list of `hash_columns` (order matters for concatenation).
*   `hash_seed`: The seed value used for both `hashtextextended` (SQL) and `PostgresHashtextextend` (Go).
*   `output`: Paths for the `select_csv_path` and `replication_csv_path` output files.
*   `debug`: Boolean flag to enable verbose debug logging.

## Packages

*   `cmd/seatbelt`: Main application entry point.
*   `pkg/config`: Loads and parses `config.yaml`.
*   `pkg/csvutil`: Utility for writing `map[int32]int64` to CSV.
*   `pkg/postgres_funcs`: Contains the Go implementation of `hashtextextended`.
*   `pkg/replication`: Handles the logical replication connection, message parsing, row reconstruction, and Go-side hash computation.
*   `test`: Contains integration tests and Docker setup for PostgreSQL.

## Testing

Integration tests are located in the `test/` directory.

*   **Setup:** Uses `docker-compose.yml` to spin up a PostgreSQL 17 container.
    *   `postgres-init/01-init.sql`: Initializes the database, creates `data_proof` (for potential manual use) and `data_proof_test` tables, copies sample data, and sets up necessary permissions and the main `seatbelt_pub` publication.
    *   `postgres-config.conf`: Custom PostgreSQL configuration (e.g., enabling logical replication).
*   **Execution:** The `test/run_tests.sh` script automates the testing process:
    1.  Stops/Removes old Docker containers.
    2.  Removes old PostgreSQL data volume (`.postgres_data`).
    3.  Starts the PostgreSQL container using `docker-compose up -d --build`.
    4.  Waits for PostgreSQL to become ready.
    5.  Runs `go mod tidy`.
    6.  Executes `go test -v -timeout 120s ./...` from the project root. This discovers and runs `test/integration_test.go`.
*   **`integration_test.go`:**
    *   Connects to the test database.
    *   Creates test-specific replication slot (`seatbelt_test_slot`) and publication (`seatbelt_test_pub` for `data_proof_test`) if they don't exist.
    *   Fetches hashes from `data_proof_test` using the SELECT method (dynamic SQL construction).
    *   Triggers WAL generation for `data_proof_test` (using an `UPDATE` statement).
    *   Runs the replication consumer (`pkg/replication`) configured for the test table, slot, and publication.
    *   Compares the map of hashes obtained from SELECT with the map obtained from replication.
    *   Asserts that the hashes match for all common IDs and that there are no missing IDs in either result set.

## How to Run

1.  **Prerequisites:** Docker, Docker Compose, Go (ensure version supports modules).
2.  **Build/Run Application:**
    *   Ensure `config.yaml` is configured correctly for your target database.
    *   Ensure the required PostgreSQL publication (`seatbelt_pub`) and replication slot (`seatbelt_batch_slot`) exist on the target database.
    *   Run: `go run cmd/seatbelt/main.go` (or build `go build -o seatbelt_processor cmd/seatbelt/main.go` and run `./seatbelt_processor`).
    *   Output CSV files (`select_output.csv`, `replication_output.csv` by default) will be generated.
3.  **Run Tests:**
    *   Navigate to the project root directory.
    *   Execute: `bash test/run_tests.sh`
    *   To stop containers after the test run: `bash test/run_tests.sh --down`

## Notes for LLMs

*   The core logic comparison happens in the integration test (`test/integration_test.go`), which validates the Go hash function against the SQL hash function.
*   The main application (`cmd/seatbelt/main.go`) primarily serves as a batch tool to generate comparable outputs (SELECT vs. Replication) for analysis, using the validated Go hash function.
*   Pay attention to the dynamic SQL construction in `cmd/seatbelt/main.go` (`fetchSelectHashes`) and `test/integration_test.go` (`fetchTestSelectHashes`) which relies on the configuration.
*   The replication consumer (`pkg/replication/consumer.go`) is designed to handle text-formatted replication data (`binary 'false'` plugin argument).
*   Import paths for local packages assume the Go module path is `seatbelt-source-postgres` (defined in `go.mod`). 
