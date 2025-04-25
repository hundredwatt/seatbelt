#!/bin/bash
set -e

# Make sure we're in the project root directory where go.mod resides
if [ ! -f go.mod ]; then
    echo "Error: go.mod not found. Please run this script from the project root directory."
    exit 1
fi

# Navigate to the parent test directory for docker-compose context
cd ../test

# Stop and remove existing containers
echo "Cleaning up existing containers..."
docker-compose down

# Remove PostgreSQL data to ensure clean slate with new configuration
echo "Removing existing PostgreSQL data..."
rm -rf .postgres_data

# Ensure docker-compose is running
echo "Starting Docker containers..."
docker-compose up -d --build # Add --build flag

# Wait for containers to start
echo "Waiting for PostgreSQL to be ready..."
# Improved wait logic: Check pg_isready
TIMEOUT=60 # seconds
START_TIME=$(date +%s)
while ! docker-compose exec -T postgres pg_isready -U postgres > /dev/null 2>&1; do
    if [ $(($(date +%s) - START_TIME)) -ge $TIMEOUT ]; then
        echo "Error: PostgreSQL did not become ready within $TIMEOUT seconds."
        docker-compose logs postgres
        docker-compose down
        exit 1
    fi
    echo -n "."
    sleep 2
done
echo " PostgreSQL is ready!"

# Go back to project root to run tests
cd ..

# Ensure Go dependencies are up-to-date
echo "Ensuring Go dependencies are tidy..."
go mod tidy

# Run tests with longer timeout and more verbosity
echo "Running tests..."
go test -v -timeout 120s ./...

# Option to stop containers when finished
if [ "$1" == "--down" ]; then
    echo "Stopping Docker containers..."
    cd test # Go back to test dir for docker-compose down
    docker-compose down
    cd ..
fi

echo "Test run finished." 
