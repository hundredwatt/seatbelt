-- Create test tables
CREATE TABLE IF NOT EXISTS test_table (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS data_proof (
  id SERIAL PRIMARY KEY,
  smallint_col smallint,
  bigint_col bigint,
  float_col real,
  double_col double precision,
  row_encoded TEXT,
  checksum TEXT
);

-- Copy data from CSV
COPY data_proof (id, smallint_col, bigint_col, float_col, double_col, row_encoded, checksum)
FROM '/entrypoint-initdb.d/data_20250329_172620.csv'
WITH (
    FORMAT csv,
    HEADER true,
    FORCE_NOT_NULL (id, row_encoded, checksum),
    FORCE_NULL (smallint_col, bigint_col, float_col, double_col)
);

-- Create a dedicated test table and copy data
CREATE TABLE IF NOT EXISTS data_proof_test (LIKE data_proof INCLUDING ALL);

INSERT INTO data_proof_test (id, smallint_col, bigint_col, float_col, double_col, row_encoded, checksum)
SELECT id, smallint_col, bigint_col, float_col, double_col, row_encoded, checksum
FROM data_proof;

-- Create the main publication for the application's use
CREATE PUBLICATION seatbelt_pub FOR TABLE data_proof;

-- Create the test publication 
CREATE PUBLICATION seatbelt_test_pub FOR TABLE data_proof_test;

-- Create the test slot
SELECT pg_create_logical_replication_slot('seatbelt_test_slot', 'pgoutput');

-- Grant necessary privileges
ALTER USER postgres WITH REPLICATION; 