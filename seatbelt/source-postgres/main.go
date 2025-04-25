package main

import (
	"context" // Keep for potential future use? Or remove if definitely not needed.
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourusername/seatbelt/pkg/postgres_funcs" // Correct import path based on go.mod
)

// Add debug flag and print intermediate hash values
var debug = true // Set to true for debug output

// ReplicationConsumer manages the replication connection and message handling
type ReplicationConsumer struct {
	conn            *pgconn.PgConn
	relations       map[uint32]*pglogrepl.RelationMessage
	typeMap         *pgtype.Map
	idleTimeout     time.Duration
	lastActivity    time.Time
	hashSeed        uint64 // Keep seed as uint64 internally, cast to int64 for hash func
	expectedHashes  map[int32]int64
	expectedStrings map[int32]string // Add map for expected strings from PG query
	rowsProcessed   int              // Track number of processed rows
	maxRows         int              // Max rows to process before exiting
}

// NewReplicationConsumer creates a new consumer
func NewReplicationConsumer(replConnString string, idleTimeout time.Duration, hashSeed uint64, expectedHashes map[int32]int64, expectedStrings map[int32]string, maxRows int) (*ReplicationConsumer, error) {
	// Use a separate context for connection as it's short-lived
	connCtx, connCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer connCancel()

	conn, err := pgconn.Connect(connCtx, replConnString)
	if err != nil {
		return nil, fmt.Errorf("failed to connect replication connection: %w", err)
	}

	return &ReplicationConsumer{
		conn:            conn,
		relations:       make(map[uint32]*pglogrepl.RelationMessage),
		typeMap:         pgtype.NewMap(),
		idleTimeout:     idleTimeout,
		lastActivity:    time.Now(),
		hashSeed:        hashSeed,
		expectedHashes:  expectedHashes,
		expectedStrings: expectedStrings, // Assign the new map
		rowsProcessed:   0,
		maxRows:         maxRows,
	}, nil
}

// Start begins consuming the replication stream
func (c *ReplicationConsumer) Start(ctx context.Context, slotName, publicationName string) error {
	log.Printf("Starting replication consumer loop...")

	// Start the replication stream
	pluginArgs := []string{
		"proto_version '1'",
		fmt.Sprintf("publication_names '%s'", publicationName),
		"binary 'false'", // Request text data
	}
	err := pglogrepl.StartReplication(ctx, c.conn, slotName, 0, pglogrepl.StartReplicationOptions{
		Mode:       pglogrepl.LogicalReplication,
		PluginArgs: pluginArgs,
	})
	if err != nil {
		// Specific error checking for common issues
		if strings.Contains(err.Error(), "does not exist") {
			log.Printf("Replication slot '%s' does not exist. Please create it using SQL.", slotName)
		} else if strings.Contains(err.Error(), "could not connect") || strings.Contains(err.Error(), "connection refused") {
			log.Printf("Error connecting replication stream. Check DB status and replication connection string.")
		} else if strings.Contains(err.Error(), "publication") && strings.Contains(err.Error(), "does not exist") {
			log.Printf("Publication '%s' does not exist. Please create it using SQL.", publicationName)
		}
		return fmt.Errorf("failed to start replication stream: %w", err)
	}
	log.Printf("Successfully started replication stream with slot %s and publication %s", slotName, publicationName)

	clientXLogPos := pglogrepl.LSN(0)
	standbyMessageTimeout := time.Second * 10
	nextStandbyMessageDeadline := time.Now().Add(standbyMessageTimeout)
	c.lastActivity = time.Now()

	for {
		// Check context cancellation first
		if ctx.Err() != nil {
			log.Printf("Context cancelled, exiting replication loop: %v", ctx.Err())
			return ctx.Err()
		}

		if c.idleTimeout > 0 && time.Since(c.lastActivity) > c.idleTimeout {
			log.Printf("No activity detected for %v, stopping replication consumer", c.idleTimeout)
			return nil // Normal exit on idle
		}

		// Check if we've processed enough rows
		if c.maxRows > 0 && c.rowsProcessed >= c.maxRows {
			log.Printf("Processed %d rows (max: %d), exiting", c.rowsProcessed, c.maxRows)
			return nil
		}

		if time.Now().After(nextStandbyMessageDeadline) {
			standbyCtx, standbyCancel := context.WithTimeout(ctx, standbyMessageTimeout) // Use main ctx for timeout
			err := pglogrepl.SendStandbyStatusUpdate(standbyCtx, c.conn, pglogrepl.StandbyStatusUpdate{WALWritePosition: clientXLogPos})
			standbyCancel()
			if err != nil {
				log.Printf("Error sending standby status update: %v. Attempting to continue.", err)
				if ctx.Err() != nil { // Check if context cancelled during send
					return ctx.Err()
				}
			} else {
				// log.Println("Sent Standby status update")
			}
			nextStandbyMessageDeadline = time.Now().Add(standbyMessageTimeout)
		}

		// Use a receive timeout slightly shorter than the standby message deadline
		receiveCtx, receiveCancel := context.WithDeadline(context.Background(), nextStandbyMessageDeadline.Add(-1*time.Second))
		msg, err := c.conn.ReceiveMessage(receiveCtx)
		receiveCancel()

		if err != nil {
			if pgconn.Timeout(err) {
				continue // Normal timeout, loop again
			}
			if ctx.Err() != nil { // Check if the main context was cancelled after ReceiveMessage returned
				log.Printf("Context cancelled during or after ReceiveMessage, exiting loop: %v", ctx.Err())
				return ctx.Err()
			}
			var netErr net.Error
			if errors.As(err, &netErr) {
				log.Printf("Network error receiving message: %v", err)
				return fmt.Errorf("network error receiving message: %w", err) // Likely fatal
			}
			// Other receive error
			return fmt.Errorf("failed to receive replication message: %w", err)
		}

		// Update last activity time whenever any message is received
		c.lastActivity = time.Now()

		switch msg := msg.(type) {
		case *pgproto3.CopyData:
			switch msg.Data[0] {
			case pglogrepl.PrimaryKeepaliveMessageByteID:
				pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
				if err != nil {
					log.Printf("Failed to parse keepalive message: %v", err)
					continue // Log and continue
				}
				if pkm.ReplyRequested {
					nextStandbyMessageDeadline = time.Time{} // Send standby update immediately
				}

			case pglogrepl.XLogDataByteID:
				xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
				if err != nil {
					log.Printf("Failed to parse XLogData: %v", err)
					continue
				}

				logicalMsg, err := pglogrepl.Parse(xld.WALData)
				if err != nil {
					log.Printf("Failed to parse logical replication message: %v", err)
					continue
				}

				switch logicalMsg := logicalMsg.(type) {
				case *pglogrepl.RelationMessage:
					c.relations[logicalMsg.RelationID] = logicalMsg

				case *pglogrepl.BeginMessage:
					// Transaction start
				case *pglogrepl.CommitMessage:
					// Transaction commit
				case *pglogrepl.InsertMessage:
					if rel, ok := c.relations[logicalMsg.RelationID]; ok {
						if rel.RelationName == "data_proof" && (rel.Namespace == "public" || rel.Namespace == "") {
							values := c.parseRow(rel, logicalMsg.Tuple.Columns)
							c.processDataProofRow(values)
						}
					} else {
						log.Printf("Warning: Received INSERT for unknown relation ID: %d", logicalMsg.RelationID)
					}

				case *pglogrepl.UpdateMessage: // Ignore other message types for this example
				case *pglogrepl.DeleteMessage:
				case *pglogrepl.TruncateMessage:
				default:
					log.Printf("Received unhandled logical message type: %T", logicalMsg)
				}

				// Acknowledge LSN
				clientXLogPos = xld.WALStart + pglogrepl.LSN(len(xld.WALData))
			}
		default:
			log.Printf("Received unexpected physical message type: %T", msg)
		}
	}
}

// parseRow - Parses columns, expecting text format for data proof rows
func (c *ReplicationConsumer) parseRow(relation *pglogrepl.RelationMessage, columns []*pglogrepl.TupleDataColumn) map[string]interface{} {
	values := make(map[string]interface{})
	// Define columns needed for hashing explicitly here or elsewhere
	// requiredCols := map[string]bool{
	// 	"id":           true, // ID is handled separately but good to note
	// 	"smallint_col": true,
	// 	"bigint_col":   true,
	// 	"float_col":    true,
	// 	"double_col":   true,
	// }

	for idx, col := range columns {
		if idx >= len(relation.Columns) {
			log.Printf("Warning: Column index %d out of bounds for relation %s.%s (max %d)", idx, relation.Namespace, relation.RelationName, len(relation.Columns)-1)
			continue
		}
		colInfo := relation.Columns[idx]
		colName := colInfo.Name

		switch col.DataType {
		case 'n': // null
			values[colName] = nil // Use nil to represent SQL NULL
		case 'u': // unchanged toast - Should not occur often with text mode, but handle defensively
			log.Printf("Warning: Column '%s' has UNCHANGED_TOAST value ('u'), treating as NULL.", colName)
			values[colName] = nil
		case 't': // text
			// Store the raw bytes, conversion to string happens in processDataProofRow if needed
			values[colName] = col.Data
		case 'b': // binary - Should primarily be the 'id' column now
			// Store the raw bytes for ID decoding later
			values[colName] = col.Data
		default:
			log.Printf("Warning: Unknown column data type '%c' for column '%s'", col.DataType, colName)
			values[colName] = nil // Treat unknown as null for safety
		}
	}
	return values
}

// processDataProofRow - Concatenates text columns and hashes using PostgresHashtextextend
func (c *ReplicationConsumer) processDataProofRow(values map[string]interface{}) {
	// 1. Extract ID (now received in TEXT format due to binary 'false')
	idVal, idOk := values["id"]
	if !idOk || idVal == nil {
		log.Printf("Error: Missing or NULL 'id' column in data_proof row: %+v", values)
		return
	}

	// Decode the ID from its text representation
	var id int32
	if idBytes, ok := idVal.([]byte); ok {
		idStr := string(idBytes)
		parsedID, err := strconv.ParseInt(idStr, 10, 32) // Parse as base-10, 32-bit int
		if err != nil {
			log.Printf("Error: Could not parse 'id' column from text '%s': %v", idStr, err)
			return
		}
		id = int32(parsedID)
	} else {
		// This case should not happen if parseRow puts []byte for text
		log.Printf("Error: Could not decode 'id' column (expected text/bytes): %T %+v", idVal, idVal)
		return
	}

	// 2. Extract TEXT representation from replication stream for hashing columns
	colNames := []string{"smallint_col", "bigint_col", "float_col", "double_col"}
	var streamTextValues []string // Store text values from stream in order

	for _, name := range colNames {
		val, ok := values[name]
		if !ok {
			log.Printf("Error: Missing column '%s' for ID %d. Cannot compute hash.", name, id)
			return // Cannot compute hash if a column is missing
		}
		if val == nil {
			streamTextValues = append(streamTextValues, "") // Use empty string for NULL
		} else if bytesVal, typeOk := val.([]byte); typeOk {
			// Data type 't' (text) provides bytes, convert to string
			streamTextValues = append(streamTextValues, string(bytesVal))
		} else {
			// This case should ideally not happen if parseRow works correctly for text
			log.Printf("Error: Unexpected type for column '%s' (ID %d): %T. Treating as empty string.", name, id, val)
			streamTextValues = append(streamTextValues, "")
		}
	}

	// 3. Concatenate text values from stream
	var builder strings.Builder
	for _, s := range streamTextValues {
		builder.WriteString(s)
	}
	streamConcatenatedString := builder.String()

	// 4. Hash the concatenated string from the stream
	computedHash := postgres_funcs.PostgresHashtextextend(streamConcatenatedString, int64(c.hashSeed))

	// 5. Compare and Log
	expectedHash, hashFound := c.expectedHashes[id]
	expectedString, stringFound := c.expectedStrings[id]
	matchStatus := "MISMATCH"

	if !hashFound || !stringFound {
		matchStatus = "EXPECTED_MISSING"
	} else if expectedHash == computedHash {
		matchStatus = "MATCH"
	}

	// Enhanced logging - always show both strings if debugging or mismatching
	if debug || matchStatus != "MATCH" {
		log.Printf("Result ID %d:", id)
		if stringFound {
			log.Printf("  Expected String (PG): '%s'", expectedString)
		} else {
			log.Printf("  Expected String (PG): <NOT FOUND>")
		}
		log.Printf("  Stream String (GO)  : '%s'", streamConcatenatedString)
		if expectedString != streamConcatenatedString && stringFound {
			log.Printf("  *** STRINGS DIFFER ***")
		}
		if hashFound {
			log.Printf("  PG_Hash (expected)  : %d", expectedHash)
		} else {
			log.Printf("  PG_Hash (expected)  : <NOT FOUND>")
		}
		log.Printf("  GO_Hash (computed)  : %d", computedHash)
		log.Printf("  Status              : %s", matchStatus)
	}

	// Increment processed rows counter
	c.rowsProcessed++
}

// Close closes the replication connection
func (c *ReplicationConsumer) Close() error {
	if c.conn != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		log.Println("Closing replication connection...")
		err := c.conn.Close(closeCtx)
		if err != nil {
			log.Printf("Error closing replication connection: %v", err)
		} else {
			log.Println("Replication connection closed.")
		}
		c.conn = nil
		return err
	}
	return nil
}

// Modify fetchExpectedHashes to get hash AND the concatenated string from PostgreSQL
func fetchExpectedHashes(ctx context.Context, pool *pgxpool.Pool, seed uint64) (map[int32]int64, map[int32]string, error) {
	log.Println("Fetching expected hashes and concatenated strings from PostgreSQL...")
	// Query selects the id, the concatenated string, and the hash of that string
	query := `
	WITH concatenated AS (
		SELECT
			id,
			(
				COALESCE(smallint_col::text, '') ||
				COALESCE(bigint_col::text, '')   ||
				COALESCE(float_col::text, '')  ||
				COALESCE(double_col::text, '')
			) AS text_val
		FROM
			data_proof
	)
	SELECT
		id,
		text_val,
		hashtextextended(text_val, $1) AS text_hash
	FROM
		concatenated;
	`
	// Pass seed as int64
	rows, err := pool.Query(ctx, query, int64(seed))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query expected hashes/strings: %w", err)
	}
	defer rows.Close()

	expectedHashes := make(map[int32]int64)
	expectedStrings := make(map[int32]string) // Map to store the strings from PG

	for rows.Next() {
		var id int32
		var textVal pgtype.Text // Use pgtype.Text to handle potential NULL strings correctly
		var hash pgtype.Int8
		// Adjust Scan arguments
		if err := rows.Scan(&id, &textVal, &hash); err != nil {
			log.Printf("Error scanning row for expected hash/string: %v", err)
			continue
		}

		if hash.Valid {
			expectedHashes[id] = hash.Int64
		} else {
			log.Printf("Warning: Received NULL expected hash for ID %d", id)
		}

		if textVal.Valid {
			expectedStrings[id] = textVal.String
		} else {
			// This shouldn't happen with the COALESCE in the query, but handle defensively
			log.Printf("Warning: Received NULL expected string for ID %d", id)
			expectedStrings[id] = "" // Store empty string if PG somehow returned NULL
		}

		if debug && hash.Valid && textVal.Valid {
			// log.Printf("DEBUG: PG ID %d - String '%s' -> Hash %d", id, textVal.String, hash.Int64)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error iterating expected hash/string rows: %w", err)
	}

	log.Printf("Fetched %d expected hashes and strings.", len(expectedHashes))
	return expectedHashes, expectedStrings, nil // Return both maps
}

// triggerReplicationEvents runs SQL to force existing data into WAL
func triggerReplicationEvents(ctx context.Context, pool *pgxpool.Pool) error {
	log.Println("Triggering replication events for data_proof table...")
	sql := `
	BEGIN;
	CREATE TEMP TABLE tmp_dp AS SELECT * FROM data_proof;
	TRUNCATE data_proof;
	INSERT INTO data_proof SELECT * FROM tmp_dp;
	DROP TABLE tmp_dp;
	COMMIT;
	`
	_, err := pool.Exec(ctx, sql)
	if err != nil {
		// Check for specific errors like permissions
		if strings.Contains(err.Error(), "permission denied for table") {
			log.Println("Permission denied during TRUNCATE/INSERT. Ensure DB user has correct privileges.")
		}
		return fmt.Errorf("failed to execute trigger SQL: %w", err)
	}
	log.Println("Successfully triggered replication events.")
	return nil
}

func main() {
	// --- Configuration ---
	// Use a separate connection string for standard operations (no replication=database)
	stdConnString := os.Getenv("PG_STD_CONN_STRING")
	if stdConnString == "" {
		stdConnString = "postgres://postgres:postgres@localhost:55810/seatbelt" // Default without replication mode
		log.Printf("Using default PG_STD_CONN_STRING: %s", stdConnString)
	}

	// Replication connection string remains the same
	replConnString := os.Getenv("PG_REPL_CONN_STRING")
	if replConnString == "" {
		replConnString = "postgres://postgres:postgres@localhost:55810/seatbelt?replication=database"
		log.Printf("Using default PG_REPL_CONN_STRING: %s", replConnString)
	}

	slotName := os.Getenv("PG_SLOT_NAME")
	if slotName == "" {
		slotName = "seatbelt_hash_slot"
		log.Printf("Using default PG_SLOT_NAME: %s", slotName)
	}

	publicationName := os.Getenv("PG_PUBLICATION")
	if publicationName == "" {
		publicationName = "seatbelt_pub"
		log.Printf("Using default PG_PUBLICATION: %s", publicationName)
	}

	idleTimeoutStr := os.Getenv("IDLE_TIMEOUT")
	idleTimeout := 30 * time.Second // Default timeout
	if idleTimeoutStr != "" {
		if duration, err := time.ParseDuration(idleTimeoutStr); err == nil {
			if duration > 0 {
				idleTimeout = duration
			} else {
				log.Printf("IDLE_TIMEOUT is zero or negative, disabling idle timeout.")
				idleTimeout = 0 // 0 disables idle timeout
			}
		} else {
			log.Printf("Warning: Invalid IDLE_TIMEOUT format '%s', using default %v", idleTimeoutStr, idleTimeout)
		}
	}

	const hashSeed = 42

	log.Printf("Configuration: Slot=%s, Publication=%s, IdleTimeout=%v, HashSeed=%d", slotName, publicationName, idleTimeout, hashSeed)
	log.Println("---")

	// Context for overall application lifecycle and shutdown signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure cancellation propagates

	// Handle termination signals (Ctrl+C)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		log.Printf("Received signal %s, initiating shutdown...", sig)
		cancel()
	}()

	// --- Setup Standard DB Connection Pool ---
	poolCtx, poolCancel := context.WithTimeout(ctx, 15*time.Second) // Timeout for pool setup
	pool, err := pgxpool.New(poolCtx, stdConnString)
	poolCancel() // Release pool setup context
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v\n", err)
	}
	defer pool.Close() // Ensure pool is closed on exit
	log.Println("Standard database connection pool established.")

	// --- Step 1: Fetch Expected Hashes and Strings ---
	fetchCtx, fetchCancel := context.WithTimeout(ctx, 30*time.Second)
	// Update function call to get both maps
	expectedHashes, expectedStrings, err := fetchExpectedHashes(fetchCtx, pool, hashSeed)
	fetchCancel()
	if err != nil {
		log.Fatalf("Failed to fetch expected hashes/strings: %v", err)
	}
	if len(expectedHashes) == 0 {
		log.Println("Warning: Fetched 0 expected hashes. Is the data_proof table populated?")
	}
	log.Println("---")

	// --- Step 2: Trigger Replication Events ---
	triggerCtx, triggerCancel := context.WithTimeout(ctx, 60*time.Second) // Timeout for trigger SQL
	err = triggerReplicationEvents(triggerCtx, pool)
	triggerCancel()
	if err != nil {
		log.Fatalf("Failed to trigger replication events: %v", err)
	}
	log.Println("---")

	// --- Step 3: Setup and Run Replication Consumer ---
	log.Println("Setting up replication consumer...")
	// Update function call to pass both maps
	consumer, err := NewReplicationConsumer(
		replConnString,
		idleTimeout,
		hashSeed,
		expectedHashes,
		expectedStrings,     // Pass the expected strings map
		len(expectedHashes)) // Pass maxRows based on fetched hashes
	if err != nil {
		log.Fatalf("Failed to create replication consumer: %v", err)
	}
	// Ensure consumer connection is closed
	defer func() {
		log.Println("Executing deferred consumer cleanup...")
		if cerr := consumer.Close(); cerr != nil {
			log.Printf("Error during deferred consumer close: %v", cerr)
		}
		log.Println("Consumer cleanup finished.")
	}()

	log.Println("Starting replication consumer...")
	// Run the consumer in a goroutine to allow main to wait for shutdown signal
	consumerErrChan := make(chan error, 1)
	go func() {
		consumerErrChan <- consumer.Start(ctx, slotName, publicationName)
	}()

	// --- Wait for shutdown or error ---
	select {
	case err := <-consumerErrChan:
		if err != nil && err != context.Canceled {
			log.Printf("Replication consumer stopped with error: %v", err)
		} else if err == context.Canceled {
			log.Println("Replication consumer stopped gracefully due to context cancellation.")
		} else {
			log.Println("Replication consumer stopped normally (likely idle timeout).")
		}
	case <-ctx.Done(): // Triggered by signal or other cancellation
		log.Println("Shutdown signal received, waiting for consumer to stop...")
		// Wait briefly for consumer goroutine to exit gracefully from context cancellation
		select {
		case err := <-consumerErrChan:
			if err != nil && err != context.Canceled {
				log.Printf("Replication consumer stopped with error during shutdown: %v", err)
			} else {
				log.Println("Replication consumer stopped gracefully after signal.")
			}
		case <-time.After(5 * time.Second): // Timeout waiting for consumer exit
			log.Println("Timeout waiting for consumer to stop.")
		}
	}

	log.Println("---")
	log.Println("Program finished.")
}

// Helper OID map remains the same
var pgTypeNameToOID = map[string]uint32{
	"int2":    pgtype.Int2OID,
	"int4":    pgtype.Int4OID,
	"int8":    pgtype.Int8OID,
	"float4":  pgtype.Float4OID,
	"float8":  pgtype.Float8OID,
	"text":    pgtype.TextOID,
	"varchar": pgtype.VarcharOID,
	"bool":    pgtype.BoolOID,
	"bpchar":  pgtype.BPCharOID, // char(n)
	"bytea":   pgtype.ByteaOID,
}
