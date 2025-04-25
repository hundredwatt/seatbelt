package test

import (
	"context"
	"log"
	"testing"
	"time"

	"seatbelt/pkg/clickhouse"

	"github.com/stretchr/testify/require"
)

// Constants for ClickHouse test configuration
const (
	// From docker-compose.yml service 'clickhouse'
	testClickHouseConnString = "clickhouse://default:pass@localhost:9000/peerdb?dial_timeout=5s&read_timeout=20s&password=pass"
	testClickHouseTableName  = "peerdb.public_data_proof"
	testClickHouseIDColumn   = "id"
)

// TestIntegration_ClickHouseSelect verifies that fetching hashes via SELECT from ClickHouse works.
func TestIntegration_ClickHouseSelect(t *testing.T) {
	log.Println("Starting TestIntegration_ClickHouseSelect...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // Timeout for the test
	defer cancel()

	// Define columns to hash (must match schema in clickhouse-init/01-init.sql)
	hashColumns := []string{"smallint_col", "bigint_col", "float_col", "double_col"}

	// --- Step 1: Fetch Hashes via SELECT from ClickHouse ---
	log.Println("Fetching hashes via SELECT from ClickHouse...")
	clickhouseHashes, err := clickhouse.FetchSelectHashes(
		ctx,
		testClickHouseConnString,
		testClickHouseTableName,
		testClickHouseIDColumn,
		hashColumns,
	)

	// --- Verify --- //
	require.NoError(t, err, "FetchSelectHashes returned an error")
	require.NotEmpty(t, clickhouseHashes, "FetchSelectHashes returned an empty map. Check if data was loaded into %s.", testClickHouseTableName)

	log.Printf("Successfully fetched %d hashes from ClickHouse table %s.", len(clickhouseHashes), testClickHouseTableName)
	log.Println("TestIntegration_ClickHouseSelect finished.")
}
