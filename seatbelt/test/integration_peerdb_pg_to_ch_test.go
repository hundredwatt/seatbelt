package test_test

import (
	"context"
	"database/sql"
	"log"
	"os"
	"testing"
	"time"

	"seatbelt/pkg/clickhouse"
	"seatbelt/pkg/postgres"
	"seatbelt/pkg/seatbelt"
	"seatbelt/pkg/row_mappers"
	"seatbelt/test/testutil"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
)

// Constants for ClickHouse test configuration
const (
	// From docker-compose.yml service 'postgres'
	testStdConnString  = "postgres://postgres:postgres@localhost:55810/seatbelt"
	testReplConnString = "postgres://postgres:postgres@localhost:55810/seatbelt?replication=database"
	testSlotName       = "seatbelt_test_slot"
	testPublication    = "seatbelt_test_pub"
	testTableName      = "public.data_proof_test"
	testIDColumn       = "id"

	// From docker-compose.yml service 'clickhouse'
	testClickHouseConnString = "clickhouse://default:pass@localhost:9000/peerdb?dial_timeout=5s&read_timeout=20s&password=pass"
	testClickHouseTableName  = "peerdb.public_data_proof"
)

var table_definition = &seatbelt.TableDefinition{
	TableName:       testTableName,
	TargetTableName: testClickHouseTableName,
	PrimaryKeyName:  testIDColumn,
	Columns: []seatbelt.ColumnMapping{
		{Name: "smallint_col", SourceType: seatbelt.ColumnTypeSmallInt, TargetType: seatbelt.ColumnTypeSmallInt},
		{Name: "bigint_col", SourceType: seatbelt.ColumnTypeBigInt, TargetType: seatbelt.ColumnTypeBigInt},
		{Name: "float_col", SourceType: seatbelt.ColumnTypeFloat, TargetType: seatbelt.ColumnTypeFloat},
		{Name: "double_col", SourceType: seatbelt.ColumnTypeDouble, TargetType: seatbelt.ColumnTypeDouble},
	},
}


var table = &seatbelt.DefaultTable{
	TableDefinition:    *table_definition,
	RowMapperAndHasher: seatbelt.NewDefaultRowMapperAndHasher(&postgres.PostgresSourceHasher{}, &clickhouse.ClickHouseTargetHasher{}, &row_mappers.PeerDBRowMapper{}),
}

func TestClickhouse_Scan(t *testing.T) {
	ctx := context.Background()
	pgxPool := openPgxPool(ctx, t)
	defer pgxPool.Close()

	ch_conn, err := sql.Open("clickhouse", testClickHouseConnString)
	if err != nil {
		t.Fatalf("Failed to open clickhouse connection: %v", err)
	}
	defer ch_conn.Close()

	source := postgres.NewPostgresSource(pgxPool)
	target := clickhouse.NewClickHouseTarget(ch_conn)

	source_scan, err := source.Scan(ctx, table)
	assert.NoError(t, err)
	defer os.Remove(source_scan.Name())

	source_extract_scan, err := source.ExtractScan(ctx, table)
	assert.NoError(t, err)
	defer os.Remove(source_extract_scan.Name())

	target_scan, err := target.Scan(ctx, table)
	assert.NoError(t, err)
	defer os.Remove(target_scan.Name())

	assert.Equal(t, int64(25), source_scan.RowCount())
	assert.Equal(t, int64(25), source_extract_scan.RowCount())
	assert.Equal(t, int64(25), target_scan.RowCount())

	assert_equal_lines(t, source_scan.File, "1,", "1,-1361447163658550079")
	assert_equal_lines(t, source_extract_scan.File, "1,", "1,-1361447163658550079,14525862213172519373")
	assert_equal_lines(t, target_scan.File, "1,", "1,14525862213172519373")

	assert_equal_lines(t, source_scan.File, "5,", "5,-6809751943371760664")
	assert_equal_lines(t, source_extract_scan.File, "5,", "5,-6809751943371760664,2558505478278155530")
	assert_equal_lines(t, target_scan.File, "5,", "5,2558505478278155530")

	assert_equal_lines(t, source_scan.File, "20,", "20,6402927007031210297")
	assert_equal_lines(t, source_extract_scan.File, "20,", "20,6402927007031210297,10750758142674176254")
	assert_equal_lines(t, target_scan.File, "20,", "20,10750758142674176254")
}

// --- Test Helper Functions ---

func openPgxPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()
	poolCtx, poolCancel := context.WithTimeout(ctx, 15*time.Second)
	pool, err := pgxpool.New(poolCtx, testStdConnString)
	poolCancel()
	if err != nil {
		t.Fatalf("Unable to create test connection pool: %v", err)
	}
	// Ping DB to ensure connection is live
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("Failed to ping test database: %v. Is docker-compose running?", err)
	}
	log.Println("Test database connection pool established.")
	return pool
}

func assert_equal_lines(t *testing.T, file *os.File, prefix string, expected string) {
	line, err := testutil.FindLineWithPrefix(file, prefix)
	assert.NoError(t, err)
	assert.Equal(t, expected, line)
}
