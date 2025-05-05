package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"

	"seatbelt/pkg/seatbelt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const postgresDatabaseName = "postgres"

type PostgresSource struct {
	conn *pgxpool.Pool
}

func NewPostgresSource(conn *pgxpool.Pool) *PostgresSource {
	return &PostgresSource{conn: conn}
}

func (s *PostgresSource) Scan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	tempDir := os.Getenv(seatbelt.EnvTempDir)
	osfile, err := os.CreateTemp(tempDir, fmt.Sprintf("seatbelt-scan-%s-*.csv", table.Name()))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)

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

	// Build a SQL query to export directly to CSV using COPY
	query := fmt.Sprintf(`
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

	// Get a connection from the pool
	conn, err := s.conn.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Execute the COPY command and stream results to the file
	commandTag, err := conn.Conn().PgConn().CopyTo(ctx, osfile, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute COPY command: %w", err)
	}

	// Set the row count
	file.SetRowCounter(commandTag.RowsAffected())

	// Reset file pointer to beginning for reading
	if err := file.Rewind(); err != nil {
		return nil, fmt.Errorf("error resetting file pointer: %w", err)
	}

	return file, nil
}

func (s *PostgresSource) ExtractScan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	tempDir := os.Getenv(seatbelt.EnvTempDir)
	osfile, err := os.CreateTemp(tempDir, fmt.Sprintf("seatbelt-extract-scan-%s-*.csv", table.Name()))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)

	// Write header
	file.WriteHeaderLine("pk,source_hash,target_hash")

	source_column_names := make([]string, len(table.SourceColumns()))
	for i, column := range table.SourceColumns() {
		source_column_names[i] = column.Name + "::text"
	}
	query := fmt.Sprintf("SELECT %s, %s FROM %s", table.PrimaryKey(), strings.Join(source_column_names, ","), table.Name())
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

		target_row_string, err := table.TransformSourceToCommon(source_column_values)
		if err != nil {
			return nil, err
		}

		source_row_hash := table.SourceHash(source_row_string)
		target_row_hash := table.TargetHash(target_row_string)

		file.WriteLine(fmt.Sprintf("%d,%s,%s", pk_val, source_row_hash, target_row_hash))
	}

	return file, nil
}

func (s *PostgresSource) StartChangeStreamConsumer(ctx context.Context, table seatbelt.Table) (seatbelt.ChangeStreamConsumer, error) {
	// TODO: Get replication-specific config (slot name, publication name, idle timeout, debug flag)
	// from somewhere (e.g., a config object passed to NewPostgresSource or environment variables).
	// For now, the consumer uses hardcoded defaults.

	// Get the base connection string from the pool's configuration.
	// The consumer will modify it to add replication parameters.
	connString := s.conn.Config().ConnConfig.ConnString()
	if connString == "" {
		// Fallback or error handling if ConnString isn't directly available
		// Might need to reconstruct from Host, Port, User, Database etc.
		// For now, return an error.
		return nil, fmt.Errorf("connection string not available in pool config")
	}

	consumer, err := NewPostgresChangeStreamConsumer(ctx, connString, table /* Add other config params */)
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres change stream consumer: %w", err)
	}

	return consumer, nil
}
