package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"seatbelt/pkg/seatbelt"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

const clickhouseDatabaseName = "clickhouse"

// ClickHouseTarget implements the seatbelt.Target interface for ClickHouse databases
type ClickHouseTarget struct {
	conn *sql.DB
}

// NewClickHouseTarget creates a new ClickHouse target with the provided database connection
func NewClickHouseTarget(conn *sql.DB) *ClickHouseTarget {
	return &ClickHouseTarget{conn: conn}
}

// Scan retrieves rows from ClickHouse and computes hashes for comparison
func (t *ClickHouseTarget) Scan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	// Get temp directory from environment variable or use default
	tempDir := os.Getenv(seatbelt.EnvTempDir)
	osfile, err := os.CreateTemp(tempDir, fmt.Sprintf("seatbelt-clickhouse-scan-%s-*.csv", table.TargetName()))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)

	// Write header
	file.WriteHeaderLine("pk,target_hash")

	// Build column list for SELECT
	var columnNames []string
	for _, col := range table.TargetColumns() {
		columnNames = append(columnNames, col.Name)
	}

	// Build the concatenation expression for hashing
	concatExpr := BuildTargetTextExpressionForHashing(table)

	// Construct query to get primary key values and compute hashes
	query := fmt.Sprintf(`
		SELECT 
			%s AS pk,
			xxh3(%s) AS target_hash
		FROM %s
	`, table.PrimaryKey(), concatExpr, table.TargetName())

	// Execute the query
	rows, err := t.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query on clickhouse: %w", err)
	}
	defer rows.Close()

	// Process results
	var rowCount int64
	for rows.Next() {
		rowCount++
		var id interface{}
		var hash uint64

		if err := rows.Scan(&id, &hash); err != nil {
			return nil, fmt.Errorf("failed to scan row from clickhouse: %w", err)
		}

		// Write row to file in format: id,hash
		_, err = fmt.Fprintf(file.File, "%v,%d\n", id, hash)
		if err != nil {
			return nil, fmt.Errorf("failed to write to file: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during row iteration from clickhouse: %w", err)
	}

	// Set the row count and rewind the file
	file.SetRowCounter(rowCount)
	if err := file.Rewind(); err != nil {
		return nil, fmt.Errorf("error resetting file pointer: %w", err)
	}

	return file, nil
}
