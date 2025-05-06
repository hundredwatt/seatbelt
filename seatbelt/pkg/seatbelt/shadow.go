package seatbelt

import (
	"context"
	"database/sql"
	"fmt"
	"log" // Added for logging errors
	"time"

	_ "github.com/marcboeker/go-duckdb/v2" // Import the v2 DuckDB driver
)

// DuckDB Configuration
const (
	AllowUnsignedExtensions = true
	Threads = 4
	MemoryLimit = "8gb"
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

// setupDuckDB connects to DuckDB and initializes the shadow table
func setupDuckDB(ctx context.Context, shadowPath string) (*sql.DB, error) {
	if shadowPath == "" {
		shadowPath = ":memory:"
	}

	// Connect to DuckDB, allowing unsigned extensions
	db, err := sql.Open("duckdb", fmt.Sprintf("%s?allow_unsigned_extensions=%t&threads=%d&memory_limit=%s", shadowPath, AllowUnsignedExtensions, Threads, MemoryLimit))
	if err != nil {
		log.Printf("Error opening DuckDB: %v", err)
		return nil, fmt.Errorf("failed to open duckdb: %w", err)
	}

	// Load the seatbelt_duckdb extension
	_, err = db.ExecContext(ctx, "LOAD seatbelt_duckdb;")
	if err != nil {
		log.Printf("Error loading seatbelt_duckdb extension: %v", err)
		// Continue anyway, maybe it's already loaded or built-in
	}

	// Ensure shadow table exists
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

	return db, nil
}

// createDataViews creates temporary views for source, target, and incremental scans
func createDataViews(ctx context.Context, db *sql.DB, data_files *DataFileSet, initialLoad bool) error {
	// For initial load, we only create source_extract and target views
	if initialLoad {
		if data_files.SourceExtractScan == nil || data_files.TargetScan == nil {
			return fmt.Errorf("source extract scan and target scan are required for initial load")
		}

		createSourceExtractViewQuery := fmt.Sprintf(`
			CREATE TEMP VIEW source_extract AS 
			SELECT 
				CAST(pk AS BIGINT) AS pk,
				CAST(source_hash AS BIGINT) AS source_signature,
				CAST(target_hash AS UBIGINT) AS target_signature
			FROM '%s';
		`, data_files.SourceExtractScan.File.Name())

		createTargetViewQuery := fmt.Sprintf(`
			CREATE TEMP VIEW target AS 
			SELECT 
				CAST(pk AS BIGINT) AS pk,
				CAST(target_hash AS UBIGINT) AS target_signature
			FROM '%s';
		`, data_files.TargetScan.File.Name())

		for _, query := range []string{createSourceExtractViewQuery, createTargetViewQuery} {
			_, err := db.ExecContext(ctx, query)
			if err != nil {
				log.Printf("Error creating view: %v\nQuery:\n%s", err, query)
				return fmt.Errorf("failed to create view: %w", err)
			}
		}
		return nil
	}

	// Regular incremental update - create all three views
	if data_files.SourceScan == nil || data_files.TargetScan == nil || data_files.SourceChanges == nil {
		return fmt.Errorf("source scan, target scan, and source changes are required for incremental update")
	}

	// Create VIEWs for the source, target, and incremental scans
	createSourceViewQuery := fmt.Sprintf(`
		CREATE TEMP VIEW source AS 
		SELECT 
			CAST(pk AS BIGINT) AS pk,
			CAST(source_hash AS BIGINT) AS source_signature
		FROM '%s';
	`, data_files.SourceScan.File.Name())
	createTargetViewQuery := fmt.Sprintf(`
		CREATE TEMP VIEW target AS 
		SELECT 
			CAST(pk AS BIGINT) AS pk,
			CAST(target_hash AS UBIGINT) AS target_signature
		FROM '%s';
	`, data_files.TargetScan.File.Name())
	createIncrementalViewQuery := fmt.Sprintf(`
		CREATE TEMP VIEW incremental AS 
		SELECT 
			CAST(pk AS BIGINT) AS pk,
			CAST(source_hash AS BIGINT) AS source_signature,
			CAST(target_hash AS UBIGINT) AS target_signature
		FROM '%s';
	`, data_files.SourceChanges.File.Name())

	for _, query := range []string{createSourceViewQuery, createTargetViewQuery, createIncrementalViewQuery} {
		_, err := db.ExecContext(ctx, query)
		if err != nil {
			log.Printf("Error creating view: %v\nQuery:\n%s", err, query)
			return fmt.Errorf("failed to create view: %w", err)
		}
	}

	return nil
}

// getUpsertQuery returns the main UPSERT query used to update the shadow table
func getUpsertQuery(initialLoad bool) string {
	if initialLoad {
		// For initial load, use source_extract in place of source and incremental
		return `
		INSERT INTO shadow
		SELECT
			COALESCE(source_extract.pk, target.pk) AS pk,
			source_extract.source_signature AS source_signature,
			target.target_signature AS target_signature,
			source_extract.source_signature AS incremental_source_signature,
			source_extract.target_signature AS incremental_target_signature,
			-- Initial load always treats records as inserts
			3 AS latest_source_operation, -- OpInsert
			CASE WHEN target.target_signature IS NULL THEN 1 ELSE 3 END AS latest_target_operation, -- OpDoesNotExist or OpInsert
			FALSE AS validation_error
		FROM source_extract
		FULL OUTER JOIN target ON source_extract.pk = target.pk
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
	}

	// Default upsert query for incremental updates
	return `
		INSERT INTO shadow
		SELECT
			COALESCE(source.pk, target.pk, incremental.pk, shadow.pk) AS pk,
			source.source_signature AS source_signature,
			target.target_signature AS target_signature,
			COALESCE(incremental.source_signature, shadow.incremental_source_signature) AS incremental_source_signature,
			COALESCE(incremental.target_signature, shadow.incremental_target_signature) AS incremental_target_signature,
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
}

// getValidationStatusCaseStmt returns the CASE statement for calculating validation status
func getValidationStatusCaseStmt() string {
	return `
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
}

// getValidationStatusLogic formats the validation status case statement with constants
func getValidationStatusLogic() string {
	return fmt.Sprintf(getValidationStatusCaseStmt(),
		StatusValid, StatusPending, StatusGone, StatusError, // 1, 2, 3, 4
		OpNoop, OpDoesNotExist, OpDelete, // 5, 6, 7
	)
}

// getMetricsQuery returns the query for calculating validation metrics
func getMetricsQuery(validationStatusLogic string) string {
	return fmt.Sprintf(`
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
}

// ExplainAnalyzeUpdateShadow runs the shadow update query with EXPLAIN ANALYZE and returns the plan
func ExplainAnalyzeUpdateShadow(ctx context.Context, cfg *Config, data_files *DataFileSet) (string, error) {
	// Setup the database and create views
	db, err := setupDuckDB(ctx, cfg.ShadowPath)
	if err != nil {
		return "", fmt.Errorf("failed to setup DuckDB: %w", err)
	}
	defer db.Close()

	// Create views for data files
	err = createDataViews(ctx, db, data_files, cfg.InitialLoad)
	if err != nil {
		return "", fmt.Errorf("failed to create data views: %w", err)
	}

	// Get the upsert query and prepend EXPLAIN ANALYZE
	upsertQuery := getUpsertQuery(cfg.InitialLoad)
	explainQuery := fmt.Sprintf("EXPLAIN ANALYZE %s", upsertQuery)

	// Run the EXPLAIN ANALYZE query
	rows, err := db.QueryContext(ctx, explainQuery)
	if err != nil {
		log.Printf("Error executing EXPLAIN ANALYZE query: %v\nQuery:\n%s", err, explainQuery)
		return "", fmt.Errorf("failed to execute EXPLAIN ANALYZE query: %w", err)
	}
	defer rows.Close()

	// Collect the output
	var plan string
	for rows.Next() {
		var label string
		var line string
		if err := rows.Scan(&label, &line); err != nil {
			return "", fmt.Errorf("error scanning explain result: %w", err)
		}
		plan += fmt.Sprintf("%s:\n%s\n", label, line)
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error iterating through explain results: %w", err)
	}

	return plan, nil
}

func UpdateShadow(ctx context.Context, cfg *Config, data_files *DataFileSet) (*ValidationMetrics, error) {
	// Setup the database and create views
	db, err := setupDuckDB(ctx, cfg.ShadowPath)
	if err != nil {
		return nil, fmt.Errorf("failed to setup DuckDB: %w", err)
	}
	defer db.Close()

	// Create views for data files
	err = createDataViews(ctx, db, data_files, cfg.InitialLoad)
	if err != nil {
		return nil, fmt.Errorf("failed to create data views: %w", err)
	}

	// Get and execute the upsert query
	upsertQuery := getUpsertQuery(cfg.InitialLoad)
	upsertStart := time.Now()
	_, err = db.ExecContext(ctx, upsertQuery)
	log.Printf("Upsert query completed in %v", time.Since(upsertStart))
	if err != nil {
		log.Printf("Error executing UPSERT query: %v\nQuery:\n%s", err, upsertQuery)
		return nil, fmt.Errorf("failed to execute upsert query: %w", err)
	}

	// Logic to calculate validation_status inline
	validationStatusLogic := getValidationStatusLogic()

	// Delete GONE rows
	deleteQuery := fmt.Sprintf(`
        DELETE FROM shadow
        WHERE (%s) = %[2]d -- StatusGone
    `, validationStatusLogic, StatusGone)

	deleteStart := time.Now()
	_, err = db.ExecContext(ctx, deleteQuery)
	log.Printf("Delete query completed in %v", time.Since(deleteStart))
	if err != nil {
		log.Printf("Error deleting GONE rows: %v\nQuery:\n%s", err, deleteQuery)
		return nil, fmt.Errorf("failed to delete gone rows: %w", err)
	}

	// Calculate metrics
	metricsQuery := getMetricsQuery(validationStatusLogic)

	metricsStart := time.Now()
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
	log.Printf("Metrics query completed in %v", time.Since(metricsStart))
	if err != nil {
		log.Printf("Error scanning metrics: %v\nQuery:\n%s", err, metricsQuery)
		return nil, fmt.Errorf("failed to scan metrics: %w", err)
	}

	return metrics, nil
}
