#!/bin/bash

set -e

# Source common functions
source "$(dirname "$0")/_common.sh"

# Main script
ensure_services_up

# Prepare databases with specific config
prepare_databases "seatbelt-conf/subset_of_columns.yml"

print_heading "2. Insert 2 rows with errors - 1 in an included column (score), 1 in an excluded column (price)"
# Insert a row and capture the ID
echo 'mysql -u mysqluser -pmysqlpw -D mysql_db -e "INSERT INTO mysql_db.demo_data (test_name, name, score, price) VALUES ('subset_of_columns-included', 'Joe', 100, 51.22); SELECT LAST_INSERT_ID() as id;"'
echo 'mysql -u mysqluser -pmysqlpw -D mysql_db -e "INSERT INTO mysql_db.demo_data (test_name, name, score, price) VALUES ('subset_of_columns-excluded', 'Jane', 101, 82.01); SELECT LAST_INSERT_ID() as id;"'
included_id=$(docker exec -e MYSQL_PWD=mysqlpw mysql_source mysql -u mysqluser -D mysql_db -e "INSERT INTO mysql_db.demo_data (test_name, name, score, price) VALUES ('subset_of_columns-included', 'Joe', 100, 51.22); SELECT LAST_INSERT_ID() as id;" | grep -v id)
excluded_id=$(docker exec -e MYSQL_PWD=mysqlpw mysql_source mysql -u mysqluser -D mysql_db -e "INSERT INTO mysql_db.demo_data (test_name, name, score, price) VALUES ('subset_of_columns-excluded', 'Jane', 101, 82.01); SELECT LAST_INSERT_ID() as id;" | grep -v id)
echo "Inserted rows with IDs: $included_id (included), $excluded_id (excluded)"
pause

print_heading "3. Run seatbelt check again - should see 2 in-flight rows"
make seatbelt_check CONFIG_FILE=seatbelt-conf/subset_of_columns.yml
pause

print_heading "4. Sync the rows to the target, then corrupt the rows (score for included, price for excluded)"
make sling_run
echo 'psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET score = 1337 WHERE id = $included_id;"'
docker exec -it postgres_sink psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET score = 1337 WHERE id = $included_id;"
echo 'psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET price = 13.37 WHERE id = $excluded_id;"'
docker exec -it postgres_sink psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET price = 13.37 WHERE id = $excluded_id;"
pause

print_heading "5. Run seatbelt check again - should see 1 error for the row with corrupted score, but no error for the row with corrupted price"
make seatbelt_check CONFIG_FILE=seatbelt-conf/subset_of_columns.yml
pause
