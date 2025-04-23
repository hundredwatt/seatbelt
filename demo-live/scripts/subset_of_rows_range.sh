#!/bin/bash

set -e

# Source common functions
source "$(dirname "$0")/_common.sh"

# Main script
ensure_services_up

# Use default configuration
prepare_databases "seatbelt-conf/default.yml"

print_heading "2. Select two rows to test - IDs 5 and 15 (valid_update and valid_datetime)"
# Query to show the rows we'll use
echo 'mysql -u mysqluser -pmysqlpw -D mysql_db -e "SELECT id, test_name, name, score FROM mysql_db.demo_data WHERE id IN (5, 15)"'
docker exec -e MYSQL_PWD=mysqlpw mysql_source mysql -u mysqluser -D mysql_db -e "SELECT id, test_name, name, score FROM mysql_db.demo_data WHERE id IN (5, 15)"
pause

print_heading "3. Run seatbelt check with range 1-10 (includes ID 5 but not ID 15)"
make seatbelt_check SEATBELT_ARGS="--range 1,10"
pause

print_heading "4. Sync the rows to the target, then corrupt both rows (score for both)"
make sling_run
echo 'Corrupting ID 5 (within range 1-10)'
docker exec -it postgres_sink psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET score = 1337 WHERE id = 5;"
echo 'Corrupting ID 15 (outside range 1-10)'
docker exec -it postgres_sink psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET score = 9999 WHERE id = 15;"
pause

print_heading "5. Run seatbelt check with range 1-10 again - should see 1 error for row with ID 5, but no error for row with ID 15"
make seatbelt_check SEATBELT_ARGS="--range 1,10"
pause

print_heading "6. Now run seatbelt check with range 11-20 - should see 1 error for row with ID 15, but not the error for row with ID 5"
touch data/sling/last_succeeded
make seatbelt_check SEATBELT_ARGS="--range 11,20"
pause 