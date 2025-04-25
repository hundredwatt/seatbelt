# PostgreSQL Replication Consumer

This project implements a PostgreSQL replication consumer using the [pglogrepl](https://github.com/jackc/pglogrepl) library. It captures and processes changes from a PostgreSQL database through logical replication.

## Features

- Parses all DML operations (INSERT, UPDATE, DELETE, TRUNCATE)
- Waits for transactions to commit before processing changes
- Processes operations with customizable callbacks
- Integration tests with Docker Compose

## Requirements

- Go 1.21 or higher
- Docker and Docker Compose for integration tests
- PostgreSQL 12 or higher with logical replication enabled

## Configuration

The application can be configured using environment variables:

- `PG_CONN_STRING`: PostgreSQL connection string with replication permissions
- `PG_SLOT_NAME`: Name of the replication slot to use (default: "seatbelt_slot")
- `PG_PUBLICATION`: Name of the publication to subscribe to (default: "seatbelt_pub")

## Running the Application

```bash
# Build the application
go build -o replication-consumer .

# Run with default settings
./replication-consumer

# Run with custom settings
PG_CONN_STRING="postgres://user:password@host:port/dbname?replication=database" \
PG_SLOT_NAME="custom_slot" \
PG_PUBLICATION="custom_pub" \
./replication-consumer
```

## Running the Tests

```bash
# Run tests with Docker (requires Docker Compose)
./test/run_tests.sh

# To stop containers after testing
./test/run_tests.sh --down
```

## Implementation Details

The implementation:

1. Creates or uses an existing replication slot
2. Establishes a logical replication connection
3. Processes WAL log entries in real-time
4. Parses relation and tuple data 
5. Waits for transaction commits before processing changes
6. Invokes callbacks for each operation
7. Tracks statistics about observed operations

## Usage

```go
// Create a handler for replication changes
handler := &DefaultChangeHandler{
    Stats: ReplicationStats{},
}

// Create and start the consumer
consumer, err := NewReplicationConsumer(connString, handler)
if err != nil {
    log.Fatalf("Failed to create consumer: %v", err)
}
defer consumer.Close()

// Start consuming replication changes
ctx := context.Background()
err = consumer.Start(ctx, "my_slot", "my_publication")
if err != nil {
    log.Fatalf("Replication failed: %v", err)
}
```

## Custom Change Handlers

You can implement your own change handlers by implementing the `ChangeHandler` interface:

```go
type MyCustomHandler struct {
    // Your fields here
}

func (h *MyCustomHandler) HandleChange(change *ReplicationChange) {
    // Process the change as needed
    switch change.Operation {
    case "INSERT":
        // Handle insert
    case "UPDATE":
        // Handle update
    case "DELETE":
        // Handle delete
    case "TRUNCATE":
        // Handle truncate
    }
}
``` 