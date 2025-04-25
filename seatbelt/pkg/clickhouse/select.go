package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"   // Assuming v2 driver
	_ "github.com/ClickHouse/clickhouse-go/v2" // Register the driver
)

// FetchSelectHashes connects to ClickHouse, executes a SELECT query to compute
// xxh3 hashes for specified columns, and returns a map of id -> hash.
func FetchSelectHashes(ctx context.Context, connStr string, tableName string, idColumn string, hashColumns []string) (map[int32]uint64, error) {
	// Basic validation
	if tableName == "" || idColumn == "" || len(hashColumns) == 0 {
		return nil, fmt.Errorf("table name, ID column, and at least one hash column are required")
	}
	if connStr == "" {
		return nil, fmt.Errorf("connection string is required")
	}

	// Construct the concatenation expression for ClickHouse
	// Ensure columns are quoted if necessary, although simple names are common
	// ClickHouse uses concat() function. We'll cast columns to String for robust concatenation.
	var concatParts []string
	for _, col := range hashColumns {
		concatParts = append(concatParts, fmt.Sprintf("CAST(%s AS String)", col))
	}
	concatExpr := fmt.Sprintf("concat(%s)", strings.Join(concatParts, ", "))

	// Construct the full SQL query
	// Using xxh3 hash function
	query := fmt.Sprintf("SELECT %s, xxh3(%s) AS computed_hash FROM %s",
		idColumn,
		concatExpr,
		tableName,
	)

	// Connect using the clickhouse-go/v2 driver
	conn, err := sql.Open("clickhouse", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open clickhouse connection: %w", err)
	}
	defer conn.Close()

	// Ping the database to verify the connection
	if err := conn.PingContext(ctx); err != nil {
		// Try to get more specific ClickHouse error if possible
		if exception, ok := err.(*clickhouse.Exception); ok {
			return nil, fmt.Errorf("failed to ping clickhouse: [%d] %s \n%s", exception.Code, exception.Message, exception.StackTrace)
		}
		return nil, fmt.Errorf("failed to ping clickhouse: %w", err)
	}

	// Execute the query
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute select query on clickhouse: %w", err)
	}
	defer rows.Close()

	// Process results
	results := make(map[int32]uint64)
	var id int32   // Assuming ID is int32 based on Postgres example
	var hash uint64 // xxh3 returns UInt64, which fits in uint64

	for rows.Next() {
		if err := rows.Scan(&id, &hash); err != nil {
			return nil, fmt.Errorf("failed to scan row from clickhouse: %w", err)
		}
		results[id] = hash
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during row iteration from clickhouse: %w", err)
	}

	return results, nil
}
