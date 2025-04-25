#!/bin/bash
set -e

# Make sure we're in the project root
cd "$(dirname "$0")/.."

# Stop and remove existing containers
echo "Cleaning up existing containers..."
docker-compose -f test/docker-compose.yml down

# Remove PostgreSQL data to ensure clean slate with new configuration
echo "Removing existing PostgreSQL data..."
rm -rf test/.postgres_data

# Ensure docker-compose is running
echo "Starting Docker containers..."
docker-compose -f test/docker-compose.yml up -d

# Wait for containers to start
echo "Waiting for PostgreSQL to be ready..."
sleep 5

# Run tests with longer timeout and more verbosity
echo "Running tests..."
go test -v -timeout 60s ./...

# Option to stop containers when finished
if [ "$1" == "--down" ]; then
    echo "Stopping Docker containers..."
    docker-compose -f test/docker-compose.yml down
fi 