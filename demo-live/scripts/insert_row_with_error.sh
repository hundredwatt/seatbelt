#!/bin/bash

set -e

export DOCKER_CLI_HINTS=false

BOLD_WHITE=$(tput bold)$(tput setaf 7)
RESET=$(tput sgr0)

print_heading() {
  echo ""
  echo "${BOLD_WHITE}$1${RESET}"
}

pause() {
  echo ""
  read -p "Press any key to continue..."
}

check_services() {
  echo "Checking if all services are up and healthy..."
  
  # Get all services that should be running
  services=$(docker compose ps --services)

  # Check if services list is empty
  if [ -z "$services" ]; then
    echo "ERROR: No services found in docker-compose.yml"
    return 1
  fi
  
  for service in $services; do
    # Check if service is running and healthy
    status=$(docker compose ps --format json $service | grep -o '"State":"[^"]*"' | cut -d'"' -f4)
    health=$(docker compose ps --format json $service | grep -o '"Health":"[^"]*"' | cut -d'"' -f4)
    
    echo "Service $service: Status=$status, Health=$health"
    
    # Check if service is running
    if [ "$status" != "running" ]; then
      echo "ERROR: Service $service is not running (status: $status)"
      return 1
    fi
    
    # If health check is available, check if service is healthy
    if [ ! -z "$health" ] && [ "$health" != "healthy" ]; then
      echo "ERROR: Service $service is not healthy (health: $health)"
      return 1
    fi
  done
  
  echo "All services are up and healthy!"
  return 0
}


print_heading "0. Ensure databases are up"
check_services
pause

print_heading "1. Prepare by syncing and verifying with seatbelt"
make seatbelt_reset
make sling_run
make seatbelt_check
touch data/sling/last_succeeded # ensure we can run seatbelt again
pause

print_heading "2. Insert a row with an error"
# Insert a row and capture the ID
echo 'mysql -u mysqluser -pmysqlpw -D mysql_db -e "INSERT INTO mysql_db.demo_data (name) VALUES ('test'); SELECT LAST_INSERT_ID() as id;"'
id=$(docker exec -e MYSQL_PWD=mysqlpw mysql_source mysql -u mysqluser -D mysql_db -e "INSERT INTO mysql_db.demo_data (name) VALUES ('test'); SELECT LAST_INSERT_ID() as id;" | grep -v id)
echo "Inserted row with ID: $id"
pause

print_heading "3. Run seatbelt check again - should see 1 in-flight row"
make seatbelt_check
pause

print_heading "4. Sync the row to the target, then corrupt the row"
make sling_run
echo 'psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET name = ''1337'' WHERE id = $id;"'
docker exec -it postgres_sink psql -U postgres -d sling -c "UPDATE sling.mysql_db_demo_data SET name = '1337' WHERE id = $id;"
pause

print_heading "5. Run seatbelt check again - should see 1 error"
make seatbelt_check
pause
