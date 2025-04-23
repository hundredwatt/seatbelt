#!/bin/bash

# Common utility functions for demo scripts
export DOCKER_CLI_HINTS=false

BOLD_WHITE=$(tput bold)$(tput setaf 7)
RESET=$(tput sgr0)

print_heading() {
  echo ""
  echo "${BOLD_WHITE}$1${RESET}"
}

pause() {
  if [ -n "$AUTO_RUN" ] && [ "$AUTO_RUN" != "false" ] && [ "$AUTO_RUN" != "0" ]; then
    return
  fi
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

# Common startup sequence
ensure_services_up() {
  print_heading "0. Ensure databases are up"
  check_services
  pause
}

prepare_databases() {
  local config_file=$1
  
  print_heading "1. Prepare by syncing and verifying with seatbelt"
  make seatbelt_reset
  make sling_run
  
  if [ -z "$config_file" ]; then
    make seatbelt_check
  else
    make seatbelt_check CONFIG_FILE="$config_file"
  fi
  
  touch data/sling/last_succeeded # ensure we can run seatbelt again
  pause
} 