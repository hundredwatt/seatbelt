package postgres

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"seatbelt/pkg/seatbelt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const postgresDatabaseName = "postgres"

type PostgresSource struct {
	conn            *pgxpool.Pool
	slotName        string
	publicationName string
}

// PostgresSourceOption configures optional behavior of a PostgresSource.
type PostgresSourceOption func(*PostgresSource)

// WithReplicationSlot sets the logical replication slot and publication used by the change stream
// consumer. Either value may be empty to keep the default.
func WithReplicationSlot(slotName, publicationName string) PostgresSourceOption {
	return func(s *PostgresSource) {
		if slotName != "" {
			s.slotName = slotName
		}
		if publicationName != "" {
			s.publicationName = publicationName
		}
	}
}

// NewPostgresSource creates a PostgreSQL source. The replication slot/publication names default to
// "seatbelt_slot" / "seatbelt_pub", can be overridden via the SEATBELT_SLOT_NAME /
// SEATBELT_PUBLICATION_NAME environment variables, and finally via WithReplicationSlot options
// (highest precedence).
func NewPostgresSource(conn *pgxpool.Pool, opts ...PostgresSourceOption) *PostgresSource {
	s := &PostgresSource{conn: conn}
	if v := os.Getenv("SEATBELT_SLOT_NAME"); v != "" {
		s.slotName = v
	}
	if v := os.Getenv("SEATBELT_PUBLICATION_NAME"); v != "" {
		s.publicationName = v
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// parallelWorkers reads and validates a worker-count environment variable, falling back to def when
// unset. It rejects non-numeric values to avoid interpolating untrusted input into SET statements.
func parallelWorkers(envVar, def string) (string, error) {
	v := os.Getenv(envVar)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return "", fmt.Errorf("%s must be a non-negative integer, got %q", envVar, v)
	}
	return strconv.Itoa(n), nil
}

func (s *PostgresSource) DataSize(ctx context.Context, table seatbelt.Table) (int64, error) {
	var totalBytes sql.NullInt64
	query := `SELECT pg_table_size($1)`
	err := s.conn.QueryRow(ctx, query, table.Name()).Scan(&totalBytes)
	if err != nil {
		// This also handles pgx.ErrNoRows, though it's not expected for this query.
		return 0, fmt.Errorf("failed to get table size for %s: %w", table.Name(), err)
	}

	if !totalBytes.Valid {
		// This can happen if the table does not exist.
		return 0, fmt.Errorf("table %s not found or size is null", table.Name())
	}

	slog.Debug("Got postgres table data size", "table", table.Name(), "size_bytes", totalBytes.Int64)

	return totalBytes.Int64, nil
}

func (s *PostgresSource) Scan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	tempDir := os.Getenv(seatbelt.EnvTempDir)
	osfile, err := os.CreateTemp(tempDir, fmt.Sprintf("seatbelt-scan-%s-*.csv", table.Name()))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)
	// Clean up the temp file on any error path; cleared once we hand the file to the caller.
	success := false
	defer func() {
		if !success {
			osfile.Close()
			os.Remove(osfile.Name())
		}
	}()

	// Split schema and table to sanitize properly
	var safeFullTableName string
	parts := strings.SplitN(table.Name(), ".", 2)
	if len(parts) == 2 {
		// We have schema.table format
		schema := pgx.Identifier{parts[0]}.Sanitize()
		tableName := pgx.Identifier{parts[1]}.Sanitize()
		safeFullTableName = schema + "." + tableName
	} else {
		// Just table name, assume public schema
		safeFullTableName = pgx.Identifier{table.Name()}.Sanitize()
	}

	threads, err := parallelWorkers("SEATBELT_POSTGRES_THREADS", "1")
	if err != nil {
		return nil, err
	}

	// Get a connection from the pool
	conn, err := s.conn.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Execute SET commands to configure parallelism first
	setParallelCostCmd := fmt.Sprintf("SET parallel_tuple_cost = 0.00001")
	setMaxWorkersCmd := fmt.Sprintf("SET max_parallel_workers_per_gather = %s", threads)

	_, err = conn.Exec(ctx, setParallelCostCmd)
	if err != nil {
		return nil, fmt.Errorf("failed to set parallel_tuple_cost: %w", err)
	}

	_, err = conn.Exec(ctx, setMaxWorkersCmd)
	if err != nil {
		return nil, fmt.Errorf("failed to set max_parallel_workers_per_gather: %w", err)
	}

	// Build just the COPY command (without the SET statements)
	copyQuery := fmt.Sprintf(`
		COPY (
			SELECT 
				%s as pk,
				hashtextextended((%s), %d) AS source_hash
			FROM %s
		) TO STDOUT WITH (FORMAT csv, HEADER)
	`,
		table.PrimaryKey(),
		table.SQLTextExpressionForSourceHashing(),
		SEED, // Using the constant from default_source_hasher.go
		safeFullTableName)

	// Execute the COPY command and stream results to the file
	slog.Debug("postgres scan query", slog.String("query", copyQuery))
	bufferedWriter := bufio.NewWriter(osfile)
	commandTag, err := conn.Conn().PgConn().CopyTo(ctx, bufferedWriter, copyQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute COPY command: %w", err)
	}
	if err := bufferedWriter.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush buffered writer: %w", err)
	}

	// Set the row count
	file.SetRowCounter(commandTag.RowsAffected())

	// Reset file pointer to beginning for reading
	if err := file.Rewind(); err != nil {
		return nil, fmt.Errorf("error resetting file pointer: %w", err)
	}

	success = true
	return file, nil
}

func (s *PostgresSource) ExtractScan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	tempDir := os.Getenv(seatbelt.EnvTempDir)
	osfile, err := os.CreateTemp(tempDir, fmt.Sprintf("seatbelt-extract-scan-%s-*.csv", table.Name()))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)
	// Clean up the temp file on any error path; cleared once we hand the file to the caller.
	success := false
	defer func() {
		if !success {
			osfile.Close()
			os.Remove(osfile.Name())
		}
	}()
	bufferedWriter := bufio.NewWriter(osfile)

	// Write header using the buffered writer
	_, err = bufferedWriter.WriteString("pk,source_hash,target_hash\n")
	if err != nil {
		return nil, fmt.Errorf("failed to write header: %w", err)
	}

	source_column_names := make([]string, len(table.SourceColumns()))
	for i, column := range table.SourceColumns() {
		source_column_names[i] = column.Name + "::text"
	}
	query := fmt.Sprintf("SELECT %s, %s FROM %s", table.PrimaryKey(), strings.Join(source_column_names, ","), table.Name())

	slog.Debug("postgres extract scan query", slog.String("query", query))
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Write the rows to the file
	for rows.Next() {
		file.IncrementRowCounter()
		row, err := rows.Values()
		if err != nil {
			return nil, err
		}
		pk_val := row[0]
		source_column_values := row[1:]

		source_row_string, err := table.FormatSource(source_column_values)
		if err != nil {
			return nil, err
		}

		target_row_string, err := table.TransformSourceToCommon(source_column_values)
		if err != nil {
			return nil, err
		}

		source_row_hash := table.SourceHash(source_row_string)
		target_row_hash := table.TargetHash(target_row_string)

		// Write line using the buffered writer
		_, err = bufferedWriter.WriteString(fmt.Sprintf("%v,%s,%s\n", pk_val, source_row_hash, target_row_hash))
		if err != nil {
			return nil, fmt.Errorf("failed to write line to buffer: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err(): %w", err)
	}

	// Flush the buffer
	if err := bufferedWriter.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush buffered writer: %w", err)
	}

	// Reset file pointer to beginning for reading
	if err := file.Rewind(); err != nil {
		return nil, fmt.Errorf("error resetting file pointer: %w", err)
	}

	success = true
	return file, nil
}

func (s *PostgresSource) StartChangeStreamConsumer(ctx context.Context, table seatbelt.Table) (seatbelt.ChangeStreamConsumer, error) {
	// Get the base connection string from the pool's configuration.
	// The consumer will modify it to add replication parameters.
	connString := s.conn.Config().ConnConfig.ConnString()
	if connString == "" {
		// Fallback or error handling if ConnString isn't directly available
		// Might need to reconstruct from Host, Port, User, Database etc.
		// For now, return an error.
		return nil, fmt.Errorf("connection string not available in pool config")
	}

	// Empty names let the consumer apply its defaults ("seatbelt_slot" / "seatbelt_pub").
	consumer, err := NewPostgresChangeStreamConsumer(ctx, connString, table, s.slotName, s.publicationName)
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres change stream consumer: %w", err)
	}

	return consumer, nil
}

func (s *PostgresSource) InspectScan(ctx context.Context, table seatbelt.Table, primaryKeys []int64) (*seatbelt.DataFile, error) {
	tempDir := os.Getenv(seatbelt.EnvTempDir)
	osfile, err := os.CreateTemp(tempDir, fmt.Sprintf("seatbelt-inspect-scan-%s-*.csv", table.Name()))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)
	// Clean up the temp file on any error path; cleared once we hand the file to the caller.
	success := false
	defer func() {
		if !success {
			osfile.Close()
			os.Remove(osfile.Name())
		}
	}()

	// Split schema and table to sanitize properly
	var safeFullTableName string
	parts := strings.SplitN(table.Name(), ".", 2)
	if len(parts) == 2 {
		// We have schema.table format
		schema := pgx.Identifier{parts[0]}.Sanitize()
		tableName := pgx.Identifier{parts[1]}.Sanitize()
		safeFullTableName = schema + "." + tableName
	} else {
		// Just table name, assume public schema
		safeFullTableName = pgx.Identifier{table.Name()}.Sanitize()
	}

	// Create properly quoted primary key list
	// For security, we must properly quote each primary key
	pksList := ""
	for i, pk := range primaryKeys {
		// Use pgx.Identifier to sanitize each primary key value
		// This prevents SQL injection attacks
		pksList += fmt.Sprintf("%d", pk)
		if i < len(primaryKeys)-1 {
			pksList += ","
		}
	}

	// Build a SQL query to export directly to CSV using COPY
	query := fmt.Sprintf(`
		COPY (
			SELECT 
				%s as pk,
				hashtextextended((%s), %d) AS source_hash,
				replace(replace(%s, E'\n', '\n'), E'\r', '\r') AS source_text
			FROM %s
			WHERE %s IN (%s)
		) TO STDOUT WITH (FORMAT csv, HEADER)
	`,
		table.PrimaryKey(),
		table.SQLTextExpressionForSourceHashing(),
		SEED, // Using the constant from default_source_hasher.go
		table.SQLTextExpressionForSourceHashing(),
		safeFullTableName,
		table.PrimaryKey(),
		pksList)

	// Get a connection from the pool
	conn, err := s.conn.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Execute the COPY command and stream results to the file
	slog.Debug("postgres inspect scan query", slog.String("query", query))
	bufferedWriter := bufio.NewWriter(osfile)
	commandTag, err := conn.Conn().PgConn().CopyTo(ctx, bufferedWriter, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute COPY command: %w", err)
	}
	if err := bufferedWriter.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush buffered writer: %w", err)
	}

	// Set the row count
	file.SetRowCounter(commandTag.RowsAffected())

	// Reset file pointer to beginning for reading
	if err := file.Rewind(); err != nil {
		return nil, fmt.Errorf("error resetting file pointer: %w", err)
	}

	success = true
	return file, nil
}

func (s *PostgresSource) InspectExtractScan(ctx context.Context, table seatbelt.Table, primaryKeys []int64) (*seatbelt.DataFile, error) {
	tempDir := os.Getenv(seatbelt.EnvTempDir)
	osfile, err := os.CreateTemp(tempDir, fmt.Sprintf("seatbelt-inspect-extract-scan-%s-*.csv", table.Name()))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)
	// Clean up the temp file on any error path; cleared once we hand the file to the caller.
	success := false
	defer func() {
		if !success {
			osfile.Close()
			os.Remove(osfile.Name())
		}
	}()

	// Write header
	file.WriteHeaderLine("pk,source_hash,target_hash,source_text,target_text")

	// Prepare the list of parameters for the IN clause
	pksList := ""
	for i, pk := range primaryKeys {
		pksList += fmt.Sprintf("%d", pk)
		if i < len(primaryKeys)-1 {
			pksList += ","
		}
	}

	source_column_names := make([]string, len(table.SourceColumns()))
	for i, column := range table.SourceColumns() {
		source_column_names[i] = column.Name + "::text"
	}

	query := fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IN (%s)",
		table.PrimaryKey(),
		strings.Join(source_column_names, ","),
		table.Name(),
		table.PrimaryKey(),
		pksList)

	slog.Debug("postgres inspect extract scan query", slog.String("query", query))
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Write the rows to the file
	for rows.Next() {
		row, err := rows.Values()
		if err != nil {
			return nil, err
		}
		pk_val := row[0]
		source_column_values := row[1:]

		source_row_string, err := table.FormatSource(source_column_values)
		if err != nil {
			return nil, err
		}
		source_row_string = strings.ReplaceAll(source_row_string, "\n", "\\n")
		source_row_string = strings.ReplaceAll(source_row_string, "\r", "\\r")

		target_row_string, err := table.TransformSourceToCommon(source_column_values)
		if err != nil {
			return nil, err
		}
		target_row_string = strings.ReplaceAll(target_row_string, "\n", "\\n")
		target_row_string = strings.ReplaceAll(target_row_string, "\r", "\\r")

		source_row_hash := table.SourceHash(source_row_string)
		target_row_hash := table.TargetHash(target_row_string)

		// Escape source and target text for CSV output
		source_row_string = escapeCSVField(source_row_string)
		target_row_string = escapeCSVField(target_row_string)

		file.WriteLine(fmt.Sprintf("%v,%s,%s,%s,%s",
			pk_val,
			source_row_hash,
			target_row_hash,
			source_row_string,
			target_row_string))
	}

	// Reset file pointer to beginning for reading
	if err := file.Rewind(); err != nil {
		return nil, fmt.Errorf("error resetting file pointer: %w", err)
	}

	success = true
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
