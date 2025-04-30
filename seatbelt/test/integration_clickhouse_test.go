package test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"seatbelt/pkg/clickhouse"
	"seatbelt/pkg/seatbelt"
	"seatbelt/test/testutil"

	"github.com/stretchr/testify/assert"
)

// Constants for ClickHouse test configuration
const (
	// From docker-compose.yml service 'clickhouse'
	testClickHouseConnString = "clickhouse://default:pass@localhost:9000/peerdb?dial_timeout=5s&read_timeout=20s&password=pass"
	testClickHouseTableName  = "peerdb.public_data_proof"
	testClickHouseIDColumn   = "id"
)

var test_clickhouse_table_definition = &seatbelt.TableDefinition{
	TableName:      testClickHouseTableName,
	PrimaryKeyName: testClickHouseIDColumn,
	Columns: []seatbelt.ColumnMapping{
		{Name: "smallint_col", SourceType: seatbelt.ColumnTypeSmallInt, TargetType: seatbelt.ColumnTypeSmallInt},
		{Name: "bigint_col", SourceType: seatbelt.ColumnTypeBigInt, TargetType: seatbelt.ColumnTypeBigInt},
		{Name: "float_col", SourceType: seatbelt.ColumnTypeFloat, TargetType: seatbelt.ColumnTypeFloat},
		{Name: "double_col", SourceType: seatbelt.ColumnTypeDouble, TargetType: seatbelt.ColumnTypeDouble},
	},
}

var test_clickhouse_table = &seatbelt.DefaultTable{
	TableDefinition:    *test_clickhouse_table_definition,
	RowMapperAndHasher: seatbelt.NewDefaultRowMapperAndHasher(&testutil.MockSourceHasher{}, &clickhouse.ClickHouseTargetHasher{}),
}

func TestClickhouse_Scan(t *testing.T) {
	conn, err := sql.Open("clickhouse", testClickHouseConnString)
	if err != nil {
		t.Fatalf("Failed to open clickhouse connection: %v", err)
	}
	defer conn.Close()

	target := clickhouse.NewClickHouseTarget(conn)

	scan, err := target.Scan(context.Background(), test_clickhouse_table)
	if err != nil {
		t.Fatalf("Failed to extract scan: %v", err)
	}
	defer os.Remove(scan.Name())

	assert.Equal(t, int64(25), scan.RowCount())
	id25_line, err := testutil.FindLineWithPrefix(scan.File, "25,")
	assert.NoError(t, err)
	assert.Equal(t, "25,5503049319380937786", id25_line)
}