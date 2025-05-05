package test_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"seatbelt/pkg/clickhouse"
	"seatbelt/pkg/postgres"
	"seatbelt/pkg/row_mappers"
	"seatbelt/pkg/seatbelt"
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
	SourceDatabase: seatbelt.POSTGRES,
	TargetDatabase: seatbelt.CLICKHOUSE,
	TableName:      testTableName,
	TargetTableName:    testClickHouseTableName,
	PrimaryKeyName:     testIDColumn,
	Columns: []seatbelt.ColumnMapping{
		{Name: "smallint_col", SourceType: "smallint", TargetType: "Int16"},
		{Name: "bigint_col", SourceType: "bigint", TargetType: "Int64"},
		{Name: "float_col", SourceType: "real", TargetType: "Float32"},
		{Name: "double_col", SourceType: "double precision", TargetType: "Float64"},
	},
}

var table = &seatbelt.DefaultTable{
	TableDefinition: *table_definition,
	RowMapperAndHasher: seatbelt.NewDefaultRowMapperAndHasher(
		&postgres.PostgresSourceHasher{},
		&clickhouse.ClickHouseTargetHasher{},
		row_mappers.NewPeerDBRowMapper(*table_definition),
	),
}

func TestPeerDB_PG_To_CH(t *testing.T) {
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

	result, err := seatbelt.FetchData(ctx, &seatbelt.Config{
		Table:             table,
		Source:            source,
		Target:            target,
		InitialLoad:       true,
		TestingSourceScan: true,
	})
	defer os.Remove(result.SourceScan.File.Name())
	defer os.Remove(result.SourceExtractScan.File.Name())
	defer os.Remove(result.TargetScan.File.Name())
	assert.NoError(t, err)

	assert.Equal(t, int64(25), result.SourceScan.RowCount())
	assert.Equal(t, int64(25), result.SourceExtractScan.RowCount())
	assert.Equal(t, int64(25), result.TargetScan.RowCount())

	for i := range 25 {
		pk := i + 1

		line, err := testutil.FindLineWithPrefix(result.SourceExtractScan.File, fmt.Sprintf("%d,", pk))
		assert.NoError(t, err)
		parts := strings.Split(line, ",")
		source_hash := parts[1]
		target_hash := parts[2]

		line, err = testutil.FindLineWithPrefix(result.SourceScan.File, fmt.Sprintf("%d,", pk))
		assert.NoError(t, err)
		parts = strings.Split(line, ",")
		source_hash_2 := parts[1]
		assert.Equal(t, source_hash, source_hash_2, "source_hash mismatch for pk %d", pk)

		line, err = testutil.FindLineWithPrefix(result.TargetScan.File, fmt.Sprintf("%d,", pk))
		assert.NoError(t, err)
		parts = strings.Split(line, ",")
		target_hash_2 := parts[1]
		assert.Equal(t, target_hash, target_hash_2, "target_hash mismatch for pk %d", pk)
	}
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
