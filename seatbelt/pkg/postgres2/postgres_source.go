package postgres2

import (
	"context"
	"fmt"
	"os"
	"strings"

	"seatbelt/pkg/seatbelt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresSource struct {
	conn *pgxpool.Pool
}

func NewPostgresSource(conn *pgxpool.Pool) *PostgresSource {
	return &PostgresSource{conn: conn}
}

func (s *PostgresSource) Scan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	osfile, err := os.CreateTemp("", fmt.Sprintf("seatbelt-scan-%s-*.csv", table.Name()))
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

	// Build the concatenation part of the query for hashing
	var coalesceParts []string
	for _, col := range table.SourceColumns() {
		safeColName := pgx.Identifier{col.Name}.Sanitize()
		coalesceParts = append(coalesceParts, fmt.Sprintf("COALESCE(%s::text, '👻')", safeColName))
	}
	concatenationExpression := strings.Join(coalesceParts, " || ")

	// Build a SQL query to export directly to CSV using COPY
	query := fmt.Sprintf(`
		COPY (
			SELECT 
				%s,
				hashtextextended((%s), %d) AS computed_hash 
			FROM %s
		) TO STDOUT WITH (FORMAT csv)
	`,
		table.PrimaryKey(),
		concatenationExpression,
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
	return nil, nil
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
