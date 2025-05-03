package seatbelt

import (
	"context"
	"database/sql"
	"fmt"
	"log" // Added for logging errors

	_ "github.com/marcboeker/go-duckdb/v2" // Import the v2 DuckDB driver
)

// ValidationMetrics holds the results of the validation check.
type ValidationMetrics struct {
	SourceSize   int64
	TargetSize   int64
	SeatbeltSize int64

	ValidCount   int64
	PendingCount int64
	ErrorCount   int64
}

// Constants for Operation enum values (matching Python/DuckDB extension)
const (
	OpDoesNotExist int = 1
	OpNoop         int = 2
	OpInsert       int = 3
	OpUpdate       int = 4
	OpDelete       int = 5
)

// Constants for ValidationStatus enum values (matching Python)
const (
	StatusValid   int = 0
	StatusPending int = 1
	StatusError   int = 2
	StatusGone    int = 3
)

func UpdateShadow(ctx context.Context, cfg *Config, data_files *DataFileSet) (*ValidationMetrics, error) {
	if cfg.ShadowPath == "" {
		cfg.ShadowPath = ":memory:"
	}

	// Connect to DuckDB, allowing unsigned extensions
	db, err := sql.Open("duckdb", fmt.Sprintf("%s?allow_unsigned_extensions=true", cfg.ShadowPath))
	if err != nil {
		log.Printf("Error opening DuckDB: %v", err)
		return nil, fmt.Errorf("failed to open duckdb: %w", err)
	}
	defer db.Close()

	// Load the seatbelt_duckdb extension
	_, err = db.ExecContext(ctx, "LOAD seatbelt_duckdb;")
	if err != nil {
		log.Printf("Error loading seatbelt_duckdb extension: %v", err)
		// Continue anyway, maybe it's already loaded or built-in
		// return nil, fmt.Errorf("failed to load seatbelt_duckdb extension: %w", err)
	}

	// Ensure shadow table exists
	// NOTE: Using BIGINT for signatures initially. Adjust if VARCHAR or UNION types are needed.
	// The UNION type from Python isn't directly mapable here without more complex handling.
	_, err = db.ExecContext(ctx, `
        CREATE TABLE IF NOT EXISTS shadow (
            pk BIGINT PRIMARY KEY,
            source_signature BIGINT,
            target_signature UBIGINT,
            incremental_source_signature BIGINT,
            incremental_target_signature UBIGINT,
            source_operation UTINYINT,
            target_operation UTINYINT,
            validation_error BOOLEAN
        )
    `)
	if err != nil {
		log.Printf("Error creating shadow table: %v", err)
		return nil, fmt.Errorf("failed to create shadow table: %w", err)
	}

	// Create VIEWs for the source, target, and incremental scans
	createSourceViewQuery := fmt.Sprintf(`
		CREATE TEMP VIEW source AS 
		SELECT 
			CAST(column0 AS BIGINT) AS pk,
			CAST(column1 AS BIGINT) AS source_signature
		FROM '%s';
	`, data_files.SourceScan.File.Name())
	createTargetViewQuery := fmt.Sprintf(`
		CREATE TEMP VIEW target AS 
		SELECT 
			CAST(column0 AS BIGINT) AS pk,
			CAST(column1 AS UBIGINT) AS target_signature
		FROM '%s';
	`, data_files.TargetScan.File.Name())
	createIncrementalViewQuery := fmt.Sprintf(`
		CREATE TEMP VIEW incremental AS 
		SELECT 
			CAST(column0 AS BIGINT) AS pk,
			CAST(column1 AS BIGINT) AS source_signature,
			CAST(column2 AS UBIGINT) AS target_signature
		FROM '%s';
	`, data_files.SourceChanges.File.Name())
	for _, query := range []string{createSourceViewQuery, createTargetViewQuery, createIncrementalViewQuery} {
		_, err = db.ExecContext(ctx, query)
		if err != nil {
			log.Printf("Error creating view: %v\nQuery:\n%s", err, query)
			return nil, fmt.Errorf("failed to create view: %w", err)
		}
	}

	// Main UPSERT logic
	// Using *_int versions of functions assuming BIGINT signatures.
	// Use check_for_validation_error_with_row_integrity which matches the Python logic.
	upsertQuery := `
        INSERT INTO shadow
        SELECT
            COALESCE(source.pk, target.pk, incremental.pk, shadow.pk) AS pk,
            source.source_signature AS source_signature,
            target.target_signature AS target_signature,
            incremental.source_signature AS incremental_source_signature,
            incremental.target_signature AS incremental_target_signature,
            -- Use specific signature type function, assuming INT here
            determine_source_operation_int(
                source.source_signature,
                shadow.source_signature
            ) AS latest_source_operation,
            determine_source_operation_uint(
                target.target_signature,
                shadow.target_signature
            ) AS latest_target_operation,
            check_for_validation_error_with_row_integrity(
                latest_source_operation,
                shadow.source_operation,
                latest_target_operation,
                shadow.target_operation,
                COALESCE(shadow.validation_error, FALSE), -- Provide default for new rows
                 -- Use specific signature type function, assuming INT here
                verify_row_integrity_i_u(
                    incremental.source_signature,
                    incremental.target_signature,
                    source.source_signature,
                    target.target_signature
                )
            ) AS validation_error
        FROM source
        FULL OUTER JOIN shadow ON source.pk = shadow.pk
        FULL OUTER JOIN target ON COALESCE(source.pk, shadow.pk) = target.pk
        FULL OUTER JOIN incremental ON COALESCE(source.pk, shadow.pk, target.pk) = incremental.pk
        -- WHERE clause for partitioning/range can be added here if needed
        ORDER BY pk
        ON CONFLICT (pk) DO UPDATE SET
            source_signature = excluded.source_signature,
            target_signature = excluded.target_signature,
            incremental_source_signature = excluded.incremental_source_signature,
            incremental_target_signature = excluded.incremental_target_signature,
            source_operation = excluded.source_operation,
            target_operation = excluded.target_operation,
            validation_error = excluded.validation_error;
    `

	_, err = db.ExecContext(ctx, upsertQuery)
	if err != nil {
		log.Printf("Error executing UPSERT query: %v\nQuery:\n%s", err, upsertQuery)
		return nil, fmt.Errorf("failed to execute upsert query: %w", err)
	}

	// Logic to calculate validation_status inline (replicating Python UDF)
	// Assumes verify_row_integrity_int is the correct function for the signature types
	validationStatusCaseStmt := `
        CASE
            WHEN validation_error THEN %[4]d -- StatusError
            ELSE
                CASE
                    WHEN (
                        -- Pending conditions from Python UDF
                        (source_operation NOT IN (%[5]d, %[6]d) AND target_operation IN (%[5]d, %[6]d))
                        OR
                        (
                            NOT COALESCE(verify_row_integrity_i_u(
                                incremental_source_signature,
                                incremental_target_signature,
                                source_signature,
                                target_signature
                            ), FALSE) -- Treat NULL verification as not matching
                            AND source_operation NOT IN (%[6]d, %[7]d)
                        )
                        OR
                        (source_operation IN (%[6]d, %[7]d) AND target_operation NOT IN (%[6]d, %[7]d))
                    ) THEN %[2]d -- StatusPending
                    WHEN (
                        -- Gone conditions from Python UDF
                        source_operation IN (%[6]d, %[7]d) AND target_operation IN (%[6]d, %[7]d)
                    ) THEN %[3]d -- StatusGone
                    ELSE %[1]d -- StatusValid
                END
        END
    `
	validationStatusLogic := fmt.Sprintf(validationStatusCaseStmt,
		StatusValid, StatusPending, StatusGone, StatusError, // 1, 2, 3, 4
		OpNoop, OpDoesNotExist, OpDelete, // 5, 6, 7
	)

	// Delete GONE rows
	deleteQuery := fmt.Sprintf(`
        DELETE FROM shadow
        WHERE (%s) = %[2]d -- StatusGone
    `, validationStatusLogic, StatusGone)

	_, err = db.ExecContext(ctx, deleteQuery)
	if err != nil {
		log.Printf("Error deleting GONE rows: %v\nQuery:\n%s", err, deleteQuery)
		return nil, fmt.Errorf("failed to delete gone rows: %w", err)
	}

	// Calculate metrics
	metricsQuery := fmt.Sprintf(`
        WITH StatusCTE AS (
            SELECT
                *,
                (%s) AS validation_status -- Use the same logic as the DELETE
            FROM shadow
        )
        SELECT
            COUNT(source_signature) FILTER (WHERE source_signature IS NOT NULL) AS source_size,
            COUNT(target_signature) FILTER (WHERE target_signature IS NOT NULL) AS target_size,
            COUNT(*) AS seatbelt_size,
            COUNT(*) FILTER (WHERE validation_error = TRUE) AS error_count,
            COUNT(*) FILTER (WHERE validation_status = %[2]d) AS pending_count, -- StatusPending
            COUNT(*) FILTER (WHERE validation_status = %[3]d) AS valid_count   -- StatusValid
        FROM StatusCTE;
	`, validationStatusLogic, StatusPending, StatusValid)

	metrics := &ValidationMetrics{}
	row := db.QueryRowContext(ctx, metricsQuery)
	err = row.Scan(
		&metrics.SourceSize,
		&metrics.TargetSize,
		&metrics.SeatbeltSize,
		&metrics.ErrorCount,
		&metrics.PendingCount,
		&metrics.ValidCount,
	)
	if err != nil {
		log.Printf("Error scanning metrics: %v\nQuery:\n%s", err, metricsQuery)
		return nil, fmt.Errorf("failed to scan metrics: %w", err)
	}

	return metrics, nil
}
