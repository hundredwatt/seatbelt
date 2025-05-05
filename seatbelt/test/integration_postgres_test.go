package test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"seatbelt/pkg/postgres"
	"seatbelt/pkg/seatbelt"
	"seatbelt/test/testutil"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
)

// Constants for test configuration (matching docker-compose and init script)
const (
	testStdConnString  = "postgres://postgres:postgres@localhost:55810/seatbelt"
	testReplConnString = "postgres://postgres:postgres@localhost:55810/seatbelt?replication=database"
	testSlotName       = "seatbelt_test_slot"
	testPublication    = "seatbelt_test_pub"
	testTableName      = "public.data_proof_test"
	testIDColumn       = "id"
)

var table_definition = &seatbelt.TableDefinition{
	SourceDatabase: seatbelt.POSTGRES,
	TableName:      testTableName,
	PrimaryKeyName: testIDColumn,
	Columns: []seatbelt.ColumnMapping{
		{Name: "smallint_col", SourceType: "smallint"},
		{Name: "bigint_col", SourceType: "bigint"},
		{Name: "float_col", SourceType: "real"},
		{Name: "double_col", SourceType: "double precision"},
	},
}

var table = &seatbelt.DefaultTable{
	TableDefinition:    *table_definition,
	RowMapperAndHasher: seatbelt.NewDefaultRowMapperAndHasher(&postgres.PostgresSourceHasher{TableDefinition: table_definition}, &testutil.MockTargetHasher{}, &testutil.MockRowMapper{}),
}

func TestPostgres_Scan(t *testing.T) {
	pool := setupTestDB(context.Background(), t)
	defer pool.Close()

	source := postgres.NewPostgresSource(pool)

	scan, err := source.Scan(context.Background(), table)
	if err != nil {
		t.Fatalf("Failed to extract scan: %v", err)
	}
	defer os.Remove(scan.Name())

	assert.Equal(t, int64(25), scan.RowCount())
	id25_line, err := testutil.FindLineWithPrefix(scan.File, "25,")
	assert.NoError(t, err)
	assert.Equal(t, "25,3627417695111965652", id25_line)
}

func TestPostgres_ExtractScan(t *testing.T) {
	pool := setupTestDB(context.Background(), t)
	defer pool.Close()

	source := postgres.NewPostgresSource(pool)

	extract_scan, err := source.ExtractScan(context.Background(), table)
	if err != nil {
		t.Fatalf("Failed to extract scan: %v", err)
	}
	defer os.Remove(extract_scan.Name())

	assert.Equal(t, int64(25), extract_scan.RowCount())
	id25_line, err := testutil.FindLineWithPrefix(extract_scan.File, "25,")
	assert.NoError(t, err)
	assert.Equal(t, "25,3627417695111965652,215a98136ec47c93800d6115959e40f6", id25_line)
}

func TestPostgres_ConsumeChangeStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // Generous timeout for test
	defer cancel()

	pool := setupTestDB(ctx, t)
	defer pool.Close()

	source := postgres.NewPostgresSource(pool)
	consumer, err := source.StartChangeStreamConsumer(context.Background(), table)
	if err != nil {
		t.Fatalf("Failed to start change stream consumer: %v", err)
	}

	err = triggerTestReplicationEvents(ctx, t, pool, "data_proof_test", "data_proof")
	if err != nil {
		t.Fatalf("Failed to trigger replication events: %v", err)
	}

	source_changes, err := consumer.ConsumeToCompletion()
	if err != nil {
		t.Fatalf("Failed to consume change stream: %v", err)
	}
	defer os.Remove(source_changes.Name())

	assert.Equal(t, int64(25), source_changes.RowCount())
	id25_line, err := testutil.FindLineWithPrefix(source_changes.File, "25,")
	assert.NoError(t, err)
	assert.Equal(t, "25,3627417695111965652,215a98136ec47c93800d6115959e40f6", id25_line)
}

// --- Test Helper Functions ---

func setupTestDB(ctx context.Context, t *testing.T) *pgxpool.Pool {
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

// triggerTestReplicationEvents forces existing data in the *test* table into WAL
// using a more robust temp table -> truncate -> copy back method.
func triggerTestReplicationEvents(ctx context.Context, t *testing.T, pool *pgxpool.Pool, tableName string, testDataTableName string) error {
	t.Helper() // Ensure this is called within a test helper

	// Use a transaction for atomicity
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for trigger events: %w", err)
	}
	defer tx.Rollback(ctx) // Rollback on error

	// 1. Truncate the test table
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s;", tableName)
	_, err = tx.Exec(ctx, truncateSQL)
	if err != nil {
		return fmt.Errorf("failed to truncate table %s: %w", tableName, err)
	}

	// 2. Copy data from the test data table to the test table
	insertSQL := fmt.Sprintf("INSERT INTO %s SELECT * FROM %s;", tableName, testDataTableName)
	_, err = tx.Exec(ctx, insertSQL)
	if err != nil {
		return fmt.Errorf("failed to insert data from %s to %s: %w", testDataTableName, tableName, err)
	}
	log.Printf("Inserted data from %s to %s", testDataTableName, tableName)

	// Commit the transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit trigger events transaction: %w", err)
	}

	log.Printf("Successfully triggered replication events via TRUNCATE/INSERT on %s.", tableName)
	return nil
}
