package mysql

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"seatbelt/pkg/seatbelt"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLSource implements seatbelt.Source for a MySQL source database in batch (initial-load) mode.
// MySQL change-stream consumption is intentionally not implemented; validate MySQL → Postgres
// pipelines with `--initial-load`, which only needs ExtractScan and the target Scan.
type MySQLSource struct {
	conn *sql.DB
}

func NewMySQLSource(conn *sql.DB) *MySQLSource {
	return &MySQLSource{conn: conn}
}

// DSNFromURL converts a mysql:// URL (as used in Seatbelt configs) into the DSN that
// github.com/go-sql-driver/mysql expects, e.g. "user:pass@tcp(host:3306)/db".
func DSNFromURL(connURL string) (string, error) {
	u, err := url.Parse(connURL)
	if err != nil {
		return "", fmt.Errorf("invalid mysql connection string: %w", err)
	}
	if u.Scheme != "mysql" {
		return "", fmt.Errorf("expected mysql:// connection string, got scheme %q", u.Scheme)
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	host := u.Host
	dbName := strings.TrimPrefix(u.Path, "/")
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s", user, pass, host, dbName)
	if q := u.RawQuery; q != "" {
		dsn += "?" + q
	}
	return dsn, nil
}

// splitSchemaTable returns (schema, table) for a possibly database-qualified table name.
func splitSchemaTable(name string) (string, string) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0]
}

func (s *MySQLSource) DataSize(ctx context.Context, table seatbelt.Table) (int64, error) {
	schema, tableName := splitSchemaTable(table.Name())
	var size sql.NullInt64
	var err error
	if schema != "" {
		err = s.conn.QueryRowContext(ctx,
			`SELECT COALESCE(data_length + index_length, 0) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?`,
			schema, tableName).Scan(&size)
	} else {
		err = s.conn.QueryRowContext(ctx,
			`SELECT COALESCE(data_length + index_length, 0) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`,
			tableName).Scan(&size)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get mysql table size for %s: %w", table.Name(), err)
	}
	return size.Int64, nil
}

// quotedTableName returns a backtick-quoted, optionally schema-qualified table name.
func quotedTableName(name string) string {
	schema, tableName := splitSchemaTable(name)
	if schema != "" {
		return "`" + schema + "`.`" + tableName + "`"
	}
	return "`" + tableName + "`"
}

// selectColumns returns the source columns (excluding the primary key) used for hashing.
func selectColumns(table seatbelt.Table) []string {
	cols := make([]string, 0, len(table.SourceColumns()))
	for _, col := range table.SourceColumns() {
		cols = append(cols, "`"+col.Name+"`")
	}
	return cols
}

// normalizeRow converts driver []byte values to strings so canonicalization produces the same text
// form a Postgres `col::text` cast would.
func normalizeRow(vals []interface{}) []interface{} {
	out := make([]interface{}, len(vals))
	for i, v := range vals {
		switch b := v.(type) {
		case []byte:
			out[i] = string(b)
		default:
			out[i] = v
		}
	}
	return out
}

func (s *MySQLSource) queryRows(ctx context.Context, table seatbelt.Table) (*sql.Rows, error) {
	cols := selectColumns(table)
	query := fmt.Sprintf("SELECT `%s`, %s FROM %s", table.PrimaryKey(), strings.Join(cols, ","), quotedTableName(table.Name()))
	slog.Debug("mysql scan query", slog.String("query", query))
	return s.conn.QueryContext(ctx, query)
}

func (s *MySQLSource) Scan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	return s.scan(ctx, table, false)
}

func (s *MySQLSource) ExtractScan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	return s.scan(ctx, table, true)
}

// scan reads every row, hashing in Go. When extract is true it also writes the destination hash
// (computed from the mapper's canonical string) producing pk,source_hash,target_hash; otherwise it
// writes pk,source_hash.
func (s *MySQLSource) scan(ctx context.Context, table seatbelt.Table, extract bool) (*seatbelt.DataFile, error) {
	tempDir := os.Getenv(seatbelt.EnvTempDir)
	osfile, err := os.CreateTemp(tempDir, fmt.Sprintf("seatbelt-mysql-scan-%s-*.csv", table.Name()))
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

	w := bufio.NewWriter(osfile)
	if extract {
		if _, err := w.WriteString("pk,source_hash,target_hash\n"); err != nil {
			return nil, fmt.Errorf("failed to write header: %w", err)
		}
	} else {
		if _, err := w.WriteString("pk,source_hash\n"); err != nil {
			return nil, fmt.Errorf("failed to write header: %w", err)
		}
	}

	rows, err := s.queryRows(ctx, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	colTypes, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	n := len(colTypes)

	for rows.Next() {
		file.IncrementRowCounter()
		vals := make([]interface{}, n)
		ptrs := make([]interface{}, n)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("failed to scan mysql row: %w", err)
		}
		normalized := normalizeRow(vals)
		pkVal := normalized[0]
		columnValues := normalized[1:]

		sourceString, err := table.FormatSource(columnValues)
		if err != nil {
			return nil, err
		}
		sourceHash := table.SourceHash(sourceString)

		if extract {
			targetString, err := table.TransformSourceToCommon(columnValues)
			if err != nil {
				return nil, err
			}
			targetHash := table.TargetHash(targetString)
			if _, err := fmt.Fprintf(w, "%v,%s,%s\n", pkVal, sourceHash, targetHash); err != nil {
				return nil, fmt.Errorf("failed to write line: %w", err)
			}
		} else {
			if _, err := fmt.Fprintf(w, "%v,%s\n", pkVal, sourceHash); err != nil {
				return nil, fmt.Errorf("failed to write line: %w", err)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql rows error: %w", err)
	}
	if err := w.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush writer: %w", err)
	}
	if err := file.Rewind(); err != nil {
		return nil, fmt.Errorf("error resetting file pointer: %w", err)
	}

	success = true
	return file, nil
}

// StartChangeStreamConsumer is not supported for MySQL sources.
func (s *MySQLSource) StartChangeStreamConsumer(ctx context.Context, table seatbelt.Table) (seatbelt.ChangeStreamConsumer, error) {
	return nil, fmt.Errorf("change stream consumption is not supported for MySQL sources; run with --initial-load")
}
