package row_mappers

import "seatbelt/pkg/seatbelt"

var benchTableDef = seatbelt.TableDefinition{
	SourceDatabase: seatbelt.POSTGRES,
	TargetDatabase: seatbelt.CLICKHOUSE,
	TableName:      "bench_table",
	Columns: []seatbelt.ColumnMapping{
		{Name: "id", SourceType: "integer", TargetType: "Int64"},
		{Name: "name", SourceType: "text", TargetType: "String"},
		{Name: "email", SourceType: "character varying", TargetType: "String"},
		{Name: "age", SourceType: "smallint", TargetType: "Int16"},
		{Name: "score", SourceType: "real", TargetType: "Float32"},
		{Name: "balance", SourceType: "decimal", TargetType: "Decimal"},
		{Name: "active", SourceType: "boolean", TargetType: "Boolean"},
		{Name: "created_at", SourceType: "timestamp without time zone", TargetType: "DateTime"},
		{Name: "updated_at", SourceType: "timestamp with time zone", TargetType: "DateTime64"},
		{Name: "birth_date", SourceType: "date", TargetType: "Date"},
		{Name: "user_uuid", SourceType: "uuid", TargetType: "UUID"},
		{Name: "metadata", SourceType: "jsonb", TargetType: "String"},
		{Name: "description", SourceType: "text", TargetType: "String"},
		{Name: "view_count", SourceType: "bigint", TargetType: "Int64"},
		{Name: "ratio", SourceType: "double precision", TargetType: "Float64"},
		{Name: "code", SourceType: "character", TargetType: "String"},
	},
}

// benchSourceRows holds 5 postgres-style rows where all values are strings,
// matching how the mapper receives data from the replication stream.
var benchSourceRows = [5][]interface{}{
	{
		"1", "Alice Smith", "alice@example.com", "30", "9.5e+0", "1000.50000", "true",
		"2024-01-15 12:34:56", "2024-01-15 12:34:56", "1993-05-20",
		"550e8400-e29b-41d4-a716-446655440000",
		`{"role":"admin","level":5,"tags":["go","sql"]}`,
		"Senior software developer", "1500", "0.75e+0", "A",
	},
	{
		"2", "Bob Jones", "bob@example.com", "25", "7.2e+0", "500.25000", "false",
		"2023-06-30 00:00:00", "2023-06-30 00:00:00", "1998-11-12",
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		`{"role":"user","level":1}`,
		"Junior developer", "320", "0.333333e+0", "B",
	},
	{
		"3", "Carol White", "carol@example.com", "45", "1.5e+2", "9999.99000", "true",
		"2022-03-22 08:15:30", "2022-03-22 08:15:30", "1978-09-01",
		"7c9e6679-7425-40de-944b-e07fc1f90ae7",
		`{"role":"moderator","permissions":["read","write","delete"]}`,
		"Tech lead and architect", "45000", "1.5e+0", "C",
	},
	{
		"4", "Dave Brown", "dave@example.com", "32", "0.0", "0.00000", "false",
		"2021-11-05 16:45:00", "2021-11-05 16:45:00", "1991-03-15",
		"a987fbc9-4bed-3078-cf07-9141ba07c9f3",
		`{"role":"viewer"}`,
		"Product manager", "0", "0.0e+0", "D",
	},
	{
		"5", "Eve Davis", "eve@example.com", "28", "3.14159e+0", "250.12500", "true",
		"2020-07-04 23:59:59", "2020-07-04 23:59:59", "1995-12-25",
		"b9c5c2b0-4b8f-47f1-8ae9-edc4e9c5b1d2",
		`{"role":"user","settings":{"theme":"dark","lang":"en"}}`,
		"Data analyst", "8750", "3.14159e+0", "E",
	},
}

// benchTargetRows holds 5 ClickHouse-style rows with native Go types as they
// would arrive from the ClickHouse driver.
var benchTargetRows = [5][]interface{}{
	{
		int64(1), "Alice Smith", "alice@example.com", int64(30), float64(9.5), float64(1000.5), true,
		"2024-01-15 12:34:56 +0000 UTC", "2024-01-15 12:34:56 +0000 UTC", "2024-01-15",
		"550e8400-e29b-41d4-a716-446655440000",
		`{"level":5,"role":"admin","tags":["go","sql"]}`,
		"Senior software developer", int64(1500), float64(0.75), "A",
	},
	{
		int64(2), "Bob Jones", "bob@example.com", int64(25), float64(7.2), float64(500.25), false,
		"2023-06-30 00:00:00 +0000 UTC", "2023-06-30 00:00:00 +0000 UTC", "2023-06-30",
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		`{"level":1,"role":"user"}`,
		"Junior developer", int64(320), float64(0.333333), "B",
	},
	{
		int64(3), "Carol White", "carol@example.com", int64(45), float64(150.0), float64(9999.99), true,
		"2022-03-22 08:15:30 +0000 UTC", "2022-03-22 08:15:30 +0000 UTC", "1978-09-01",
		"7c9e6679-7425-40de-944b-e07fc1f90ae7",
		`{"permissions":["delete","read","write"],"role":"moderator"}`,
		"Tech lead and architect", int64(45000), float64(1.5), "C",
	},
	{
		int64(4), "Dave Brown", "dave@example.com", int64(32), float64(0.0), float64(0.0), false,
		"2021-11-05 16:45:00 +0000 UTC", "2021-11-05 16:45:00 +0000 UTC", "1991-03-15",
		"a987fbc9-4bed-3078-cf07-9141ba07c9f3",
		`{"role":"viewer"}`,
		"Product manager", int64(0), float64(0.0), "D",
	},
	{
		int64(5), "Eve Davis", "eve@example.com", int64(28), float64(3.14159), float64(250.125), true,
		"2020-07-04 23:59:59 +0000 UTC", "2020-07-04 23:59:59 +0000 UTC", "1995-12-25",
		"b9c5c2b0-4b8f-47f1-8ae9-edc4e9c5b1d2",
		`{"role":"user","settings":{"lang":"en","theme":"dark"}}`,
		"Data analyst", int64(8750), float64(3.14159), "E",
	},
}

// Golden outputs frozen from the retired native Go implementation. These are the
// canonical oracle the WASM mapper must reproduce byte-for-byte.
var goldenSource = [5]string{
	"1Alice Smithalice@example.com309.5e01000.5true2024-01-15 12:34:56.0000002024-01-15 12:34:56.0000001993-05-20550e8400-e29b-41d4-a716-446655440000{\"level\":5,\"role\":\"admin\",\"tags\":[\"go\",\"sql\"]}Senior software developer15000.75e0A",
	"2Bob Jonesbob@example.com257.2e0500.25false2023-06-30 00:00:00.0000002023-06-30 00:00:00.0000001998-11-126ba7b810-9dad-11d1-80b4-00c04fd430c8{\"level\":1,\"role\":\"user\"}Junior developer3200.333333e0B",
	"3Carol Whitecarol@example.com451.5e29999.99true2022-03-22 08:15:30.0000002022-03-22 08:15:30.0000001978-09-017c9e6679-7425-40de-944b-e07fc1f90ae7{\"permissions\":[\"read\",\"write\",\"delete\"],\"role\":\"moderator\"}Tech lead and architect450001.5e0C",
	"4Dave Browndave@example.com320.00false2021-11-05 16:45:00.0000002021-11-05 16:45:00.0000001991-03-15a987fbc9-4bed-3078-cf07-9141ba07c9f3{\"role\":\"viewer\"}Product manager00.0e0D",
	"5Eve Daviseve@example.com283.14159e0250.125true2020-07-04 23:59:59.0000002020-07-04 23:59:59.0000001995-12-25b9c5c2b0-4b8f-47f1-8ae9-edc4e9c5b1d2{\"role\":\"user\",\"settings\":{\"lang\":\"en\",\"theme\":\"dark\"}}Data analyst87503.14159e0E",
}

var goldenTarget = [5]string{
	"1Alice Smithalice@example.com309.5000001000.500000true2024-01-15 12:34:56 +0000 UTC2024-01-15 12:34:56 +0000 UTC2024-01-15550e8400-e29b-41d4-a716-446655440000{\"level\":5,\"role\":\"admin\",\"tags\":[\"go\",\"sql\"]}Senior software developer15000.750000A",
	"2Bob Jonesbob@example.com257.200000500.250000false2023-06-30 00:00:00 +0000 UTC2023-06-30 00:00:00 +0000 UTC2023-06-306ba7b810-9dad-11d1-80b4-00c04fd430c8{\"level\":1,\"role\":\"user\"}Junior developer3200.333333B",
	"3Carol Whitecarol@example.com45150.0000009999.990000true2022-03-22 08:15:30 +0000 UTC2022-03-22 08:15:30 +0000 UTC1978-09-017c9e6679-7425-40de-944b-e07fc1f90ae7{\"permissions\":[\"delete\",\"read\",\"write\"],\"role\":\"moderator\"}Tech lead and architect450001.500000C",
	"4Dave Browndave@example.com320.0000000.000000false2021-11-05 16:45:00 +0000 UTC2021-11-05 16:45:00 +0000 UTC1991-03-15a987fbc9-4bed-3078-cf07-9141ba07c9f3{\"role\":\"viewer\"}Product manager00.000000D",
	"5Eve Daviseve@example.com283.141590250.125000true2020-07-04 23:59:59 +0000 UTC2020-07-04 23:59:59 +0000 UTC1995-12-25b9c5c2b0-4b8f-47f1-8ae9-edc4e9c5b1d2{\"role\":\"user\",\"settings\":{\"lang\":\"en\",\"theme\":\"dark\"}}Data analyst87503.141590E",
}
