package postgres

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"seatbelt/pkg/seatbelt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresTarget implements seatbelt.Target for a PostgreSQL destination (e.g. the Postgres side of a
// MySQL → Postgres pipeline). It computes the destination row hash in-database with the native
// hashtextextended function so the heavy lifting stays on the server.
type PostgresTarget struct {
	conn *pgxpool.Pool
}

func NewPostgresTarget(conn *pgxpool.Pool) *PostgresTarget {
	return &PostgresTarget{conn: conn}
}

func (t *PostgresTarget) DataSize(ctx context.Context, table seatbelt.Table) (int64, error) {
	var totalBytes sql.NullInt64
	if err := t.conn.QueryRow(ctx, `SELECT pg_table_size($1)`, table.TargetName()).Scan(&totalBytes); err != nil {
		return 0, fmt.Errorf("failed to get target table size for %s: %w", table.TargetName(), err)
	}
	if !totalBytes.Valid {
		return 0, fmt.Errorf("target table %s not found or size is null", table.TargetName())
	}
	return totalBytes.Int64, nil
}

// safeQualifiedName sanitizes a possibly schema-qualified table name for interpolation.
func safeQualifiedName(name string) string {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 {
		return pgx.Identifier{parts[0]}.Sanitize() + "." + pgx.Identifier{parts[1]}.Sanitize()
	}
	return pgx.Identifier{name}.Sanitize()
}

func (t *PostgresTarget) Scan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	tempDir := os.Getenv(seatbelt.EnvTempDir)
	osfile, err := os.CreateTemp(tempDir, fmt.Sprintf("seatbelt-pg-target-scan-%s-*.csv", table.TargetName()))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)
	success := false
	defer func() {
		if !success {
			osfile.Close()
			os.Remove(osfile.Name())
		}
	}()

	conn, err := t.conn.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// The shadow table stores target signatures as UBIGINT, so reinterpret hashtextextended's signed
	// 64-bit value as unsigned (matching PostgresTargetHasher.TargetHash). Computed once in a
	// subquery to avoid hashing each row multiple times.
	query := fmt.Sprintf(`
		COPY (
			SELECT
				pk,
				(CASE WHEN h < 0 THEN h::numeric + 18446744073709551616 ELSE h::numeric END)::text AS target_hash
			FROM (
				SELECT
					%s as pk,
					hashtextextended((%s), %d) AS h
				FROM %s
			) sub
		) TO STDOUT WITH (FORMAT csv, HEADER)
	`,
		table.PrimaryKey(),
		table.SQLTextExpressionForTargetHashing(),
		SEED,
		safeQualifiedName(table.TargetName()))

	slog.Debug("postgres target scan query", slog.String("query", query))
	bufferedWriter := bufio.NewWriter(osfile)
	commandTag, err := conn.Conn().PgConn().CopyTo(ctx, bufferedWriter, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute COPY command: %w", err)
	}
	if err := bufferedWriter.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush buffered writer: %w", err)
	}

	file.SetRowCounter(commandTag.RowsAffected())
	if err := file.Rewind(); err != nil {
		return nil, fmt.Errorf("error resetting file pointer: %w", err)
	}

	success = true
	return file, nil
}
