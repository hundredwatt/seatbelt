package clickhouse

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"

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

func (t *ClickHouseTarget) DataSize(ctx context.Context, table seatbelt.Table) (int64, error) {
	targetName := table.TargetName()
	parts := strings.SplitN(targetName, ".", 2)
	var database, tableName string
	if len(parts) == 2 {
		database = parts[0]
		tableName = parts[1]
	} else {
		// If no database is specified, query for the current one.
		err := t.conn.QueryRowContext(ctx, "SELECT currentDatabase()").Scan(&database)
		if err != nil {
			return 0, fmt.Errorf("failed to get current database: %w", err)
		}
		tableName = targetName
	}

	var totalUncompressedBytes sql.NullInt64
	query := `SELECT sum(data_uncompressed_bytes) FROM system.columns WHERE database = ? AND table = ?`
	err := t.conn.QueryRowContext(ctx, query, database, tableName).Scan(&totalUncompressedBytes)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("table %s not found in system.columns", targetName)
		}
		return 0, fmt.Errorf("failed to get table uncompressed size for %s: %w", targetName, err)
	}

	if !totalUncompressedBytes.Valid {
		// This might happen if the table has no data or no columns
		return 0, fmt.Errorf("table uncompressed size is null for %s", targetName)
	}

	slog.Debug("Got clickhouse table uncompressed data size", "table", targetName, "uncompressed_size_bytes", totalUncompressedBytes.Int64)

	return totalUncompressedBytes.Int64, nil
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
	bufferedWriter := bufio.NewWriter(file.File)

	// Write header
	_, err = bufferedWriter.WriteString("pk,target_hash\n") // Simpler if DataFile doesn't handle bufio
	if err != nil {
		return nil, fmt.Errorf("failed to write header: %w", err)
	}

	// Set max_threads
	threads := os.Getenv("SEATBELT_CLICKHOUSE_THREADS")
	if threads == "" {
		threads = "4"
	}
	_, err = t.conn.ExecContext(ctx, fmt.Sprintf("SET max_threads = %s", threads))
	if err != nil {
		return nil, fmt.Errorf("failed to set max_threads: %w", err)
	}

	// Construct query to get primary key values and compute hashes
	query := fmt.Sprintf(`
		SELECT 
			%s AS pk,
			xxh3(%s) AS target_hash
		FROM %s
		WHERE _peerdb_is_deleted = 0
	`, table.PrimaryKey(), table.SQLTextExpressionForTargetHashing(), table.TargetName())

	// Execute the query
	slog.Debug("clickhouse scan query", slog.String("query", query))
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
		_, err = fmt.Fprintf(bufferedWriter, "%v,%d\n", id, hash)
		if err != nil {
			return nil, fmt.Errorf("failed to write to file: %w", err)
		}
	}

	if err := bufferedWriter.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush writer: %w", err)
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

func (t *ClickHouseTarget) InspectScan(ctx context.Context, table seatbelt.Table, primaryKeys []int64) (*seatbelt.DataFile, error) {
	tempDir := os.Getenv(seatbelt.EnvTempDir)
	osfile, err := os.CreateTemp(tempDir, fmt.Sprintf("seatbelt-inspect-clickhouse-scan-%s-*.csv", table.TargetName()))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)
	bufferedWriter := bufio.NewWriter(file.File)

	// Write header
	_, err = bufferedWriter.WriteString("pk,target_hash,target_text\n") // Simpler if DataFile doesn't handle bufio
	if err != nil {
		return nil, fmt.Errorf("failed to write header: %w", err)
	}

	// Prepare the list of parameters for the IN clause
	pksList := ""
	for i, pk := range primaryKeys {
		pksList += fmt.Sprintf("%d", pk)
		if i < len(primaryKeys)-1 {
			pksList += ","
		}
	}

	query := fmt.Sprintf(`
		SELECT 
			%s AS pk,
			xxh3(%s) AS target_hash,
			replaceAll(replaceAll(%s, '\n', '\\n'), '\r', '\\r') AS target_text
		FROM %s FINAL
		WHERE _peerdb_is_deleted = 0
		AND %s IN (%s)
	`, table.PrimaryKey(), table.SQLTextExpressionForTargetHashing(), table.SQLTextExpressionForTargetHashing(), table.TargetName(), table.PrimaryKey(), pksList)

	slog.Debug("clickhouse inspect scan query", slog.String("query", query))
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
		var text string

		if err := rows.Scan(&id, &hash, &text); err != nil {
			return nil, fmt.Errorf("failed to scan row from clickhouse: %w", err)
		}

		// Escape text for CSV output
		escapedText := escapeCSVField(text)

		// Write row to file in format: id,hash,text
		_, err = fmt.Fprintf(bufferedWriter, "%v,%d,%s\n", id, hash, escapedText)
		if err != nil {
			return nil, fmt.Errorf("failed to write to file: %w", err)
		}
	}

	if err := bufferedWriter.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush writer: %w", err)
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

// escapeCSVField properly escapes a field for CSV output
func escapeCSVField(field string) string {
	// Quote the field if it contains commas, quotes, or newlines (even escaped ones)
	if strings.ContainsAny(field, ",\"") || strings.Contains(field, "\\n") || strings.Contains(field, "\\r") {
		// Double up any quotes within the field
		field = strings.ReplaceAll(field, "\"", "\"\"")
		// Wrap the field in quotes
		return "\"" + field + "\""
	}
	return field
}
