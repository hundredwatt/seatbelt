import duckdb
import sys

print(f"Python Executable: {sys.executable}")
print(f"DuckDB Python Version: {duckdb.__version__}")

EXPECTED_CHECKSUM = 93044747

# Pass configuration when connecting
config = {'allow_unsigned_extensions' : 'true'}
db = duckdb.connect(config=config)
db.sql("LOAD '/Users/jason/Code/seatbeltdata.com/seatbelt-duckdb/build/release/extension/seatbelt_duckdb/seatbelt_duckdb.duckdb_extension'")
db.sql("CREATE TABLE data (id INTEGER, hash0 STRING, hash1 UINT32, mod8 INTEGER, random INTEGER, random_string STRING, random_float DOUBLE, random_date DATE)")
db.sql("COPY data FROM 'tmp/data-10000000-20250501-132736.txt'")

# 1. SQL implementation
db.sql("""
CREATE MACRO IF NOT EXISTS count_distinct_characters_sql(s) AS (
    length(list_distinct(string_split(s, '')))
);
""")

# 2. Python UDF
def count_distinct_characters_python_udf(string):
    return len(set(string))
db.create_function("count_distinct_characters_python_udf", count_distinct_characters_python_udf, ["VARCHAR"], "INTEGER")

def explain_analyze_and_verify(select_clause):
    query = "SELECT " + select_clause + " FROM data"
    explain = db.sql("EXPLAIN (ANALYZE) " + query).fetchall()
    for row in explain[0]:
        print(row)

    checksum = db.sql("SELECT SUM(" + select_clause + ") FROM data").fetchall()
    assert checksum[0][0] == EXPECTED_CHECKSUM

    print('-' * 100)

explain_analyze_and_verify("count_distinct_characters_python_udf(random_string)")
explain_analyze_and_verify("count_distinct_characters_sql(random_string)")
explain_analyze_and_verify("seatbelt_duckdb_count_distinct_characters(random_string)")
