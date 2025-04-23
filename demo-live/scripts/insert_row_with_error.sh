#!/bin/bash

set -e

# Source common functions
source "$(dirname "$0")/_common.sh"

# Main script
ensure_services_up

# Prepare databases with default config
prepare_databases

print_heading "2. Insert a row (will have a sync error)"
# Insert a row and capture the ID
echo 'mysql -u mysqluser -pmysqlpw -D mysql_db -e "INSERT INTO mysql_db.demo_data (name) VALUES ('test'); SELECT LAST_INSERT_ID() as id;"'
id=$(docker exec -e MYSQL_PWD=mysqlpw mysql_source mysql -u mysqluser -D mysql_db -e "INSERT INTO mysql_db.demo_data (name) VALUES ('test'); SELECT LAST_INSERT_ID() as id;" | grep -v id)
echo "Inserted row with ID: $id"
pause

print_heading "3. Run seatbelt check again - should see 1 in-flight row"
make seatbelt_check
pause

print_heading "4. Sync the row to the target - simulate a sync error by corrupting the row in the target"
make sling_run
echo 'psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET name = ''1337'' WHERE id = $id;"'
docker exec -it postgres_sink psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET name = '1337' WHERE id = $id;"
pause

print_heading "5. Run seatbelt check again - should see 1 error"
make seatbelt_check
pause
