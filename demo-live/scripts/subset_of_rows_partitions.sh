#!/bin/bash

set -e

# Source common functions
source "$(dirname "$0")/_common.sh"

# Main script
ensure_services_up

# Use default configuration
prepare_databases "seatbelt-conf/default.yml"

print_heading "2. Select two rows to test - IDs 3 and 4 (one for each partition)"
# Query to show the rows we'll use
echo 'mysql -u mysqluser -pmysqlpw -D mysql_db -e "SELECT id, test_name, name, score FROM mysql_db.demo_data WHERE id IN (3, 4)"'
docker exec -e MYSQL_PWD=mysqlpw mysql_source mysql -u mysqluser -D mysql_db -e "SELECT id, test_name, name, score FROM mysql_db.demo_data WHERE id IN (3, 4)"
echo 'ID 3 % 2 = 1 (partition 1) and ID 4 % 2 = 0 (partition 0)'
pause

print_heading "3. Run seatbelt check with partition 0 (includes ID 4 but not ID 3)"
make seatbelt_check SEATBELT_ARGS="-p 2 -n 0"
pause

print_heading "4. Sync the rows to the target, then corrupt both rows (score for both)"
make sling_run
echo 'Corrupting ID 3 (in partition 1)'
docker exec -it postgres_sink psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET score = 1337 WHERE id = 3;"
echo 'Corrupting ID 4 (in partition 0)'
docker exec -it postgres_sink psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET score = 9999 WHERE id = 4;"
pause

print_heading "5. Run seatbelt check with partition 0 again - should see 1 error for row with ID 4, but no error for row with ID 3"
make seatbelt_check SEATBELT_ARGS="-p 2 -n 0"
pause

print_heading "6. Now run seatbelt check with partition 1 - should see 1 error for row with ID 3, but not the error for row with ID 4"
touch data/sling/last_succeeded
make seatbelt_check SEATBELT_ARGS="-p 2 -n 1"
pause 