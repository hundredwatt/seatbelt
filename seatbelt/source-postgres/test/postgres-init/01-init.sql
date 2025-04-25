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
FROM '/docker-entrypoint-initdb.d/data_20250329_172620.csv'
WITH (
    FORMAT csv,
    HEADER true,
    FORCE_NOT_NULL (id, row_encoded, checksum),
    FORCE_NULL (smallint_col, bigint_col, float_col, double_col)
);

-- Create a publication for logical replication
CREATE PUBLICATION seatbelt_pub FOR TABLE test_table, data_proof;

-- Grant necessary privileges
ALTER USER postgres WITH REPLICATION; 
