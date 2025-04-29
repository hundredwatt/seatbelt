package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"seatbelt/pkg/seatbelt" // Use the core seatbelt types

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresChangeStreamConsumer implements seatbelt.ChangeStreamConsumer for PostgreSQL.
type PostgresChangeStreamConsumer struct {
	replConn        *pgconn.PgConn
	stdConn         *pgconn.PgConn // Separate connection for non-replication tasks
	table           seatbelt.Table
	relations       map[uint32]*pglogrepl.RelationMessage
	typeMap         *pgtype.Map
	idleTimeout     time.Duration
	lastActivity    time.Time
	results         map[string]seatbelt.RowHashPair // Store results here (PrimaryKey -> (SourceHash, TargetHash))
	debug           bool
	targetLSN       pglogrepl.LSN // Target LSN to reach before completing
	slotName        string        // Replication slot name
	publicationName string        // Publication name
	connString      string        // Base connection string (non-replication)
	ctx             context.Context
	cancelCtx       context.CancelFunc
}

// TODO: How should config like slotName, publicationName, connString, idleTimeout, debug be passed?
// Assuming they might be part of a larger config struct or derived. For now, hardcoding placeholders.
const defaultIdleTimeout = 10 * time.Second // Shorter default for testing
const defaultDebug = false
const defaultSlotName = "seatbelt_test_slot"       // Placeholder
const defaultPublicationName = "seatbelt_test_pub" // Placeholder

// NewPostgresChangeStreamConsumer creates a new consumer for a specific table.
func NewPostgresChangeStreamConsumer(ctx context.Context, connString string, table seatbelt.Table /* Add config params here */) (*PostgresChangeStreamConsumer, error) {
	consumerCtx, consumerCancel := context.WithCancel(ctx) // Create cancellable context for the consumer

	// --- Standard Connection Setup ---
	stdConnConfig, err := pgconn.ParseConfig(connString)
	if err != nil {
		consumerCancel()
		return nil, fmt.Errorf("failed to parse standard connection string: %w", err)
	}
	// Ensure replication mode is off for the standard connection
	delete(stdConnConfig.RuntimeParams, "replication")

	connCtx, connCancel := context.WithTimeout(consumerCtx, 10*time.Second)
	stdConn, err := pgconn.ConnectConfig(connCtx, stdConnConfig)
	connCancel()
	if err != nil {
		consumerCancel()
		return nil, fmt.Errorf("failed to establish standard connection: %w", err)
	}
	log.Println("Established standard connection for LSN checks.")

	// --- Replication Connection Setup ---
	replConnConfig, err := pgconn.ParseConfig(connString)
	if err != nil {
		stdConn.Close(context.Background()) // Close std conn if repl fails
		consumerCancel()
		return nil, fmt.Errorf("failed to parse replication connection string: %w", err)
	}
	replConnConfig.RuntimeParams["replication"] = "database"

	connCtx, connCancel = context.WithTimeout(consumerCtx, 10*time.Second)
	replConn, err := pgconn.ConnectConfig(connCtx, replConnConfig)
	connCancel()
	if err != nil {
		stdConn.Close(context.Background()) // Close std conn if repl fails
		consumerCancel()
		return nil, fmt.Errorf("failed to establish replication connection: %w", err)
	}
	log.Println("Established replication connection.")

	// TODO: Get these from config
	idleTimeout := defaultIdleTimeout
	debug := defaultDebug
	slotName := defaultSlotName
	publicationName := defaultPublicationName

	c := &PostgresChangeStreamConsumer{
		replConn:        replConn,
		stdConn:         stdConn,
		table:           table,
		relations:       make(map[uint32]*pglogrepl.RelationMessage),
		typeMap:         pgtype.NewMap(),
		idleTimeout:     idleTimeout,
		lastActivity:    time.Now(),
		results:         make(map[string]seatbelt.RowHashPair), // Initialize results map
		debug:           debug,
		slotName:        slotName,        // Store from config
		publicationName: publicationName, // Store from config
		connString:      connString,      // Store base conn string
		ctx:             consumerCtx,
		cancelCtx:       consumerCancel,
	}

	// Get initial and target LSN
	// if err := c.determineTargetLSN(); err != nil { // <-- REMOVED
	// 	c.Close() // Close connections on error // <-- REMOVED
	// 	return nil, fmt.Errorf("failed to determine target LSN: %w", err) // <-- REMOVED
	// } // <-- REMOVED

	return c, nil
}

// determineTargetLSN gets the current LSN, forces a WAL write, and gets the new LSN.
func (c *PostgresChangeStreamConsumer) determineTargetLSN() error {
	// First, get the current LSN
	_, err := c.getCurrentLSN() // We don't need the initial LSN value itself
	if err != nil {
		return fmt.Errorf("failed to get current LSN: %w", err)
	}
	// log.Printf("Current database LSN position: %s", currentLSN.String()) // Debugging

	// Next, send a WAL message to force LSN increment
	err = c.forceWalIncrement()
	if err != nil {
		return fmt.Errorf("failed to force WAL increment: %w", err)
	}

	// Get the new target LSN after the WAL increment
	c.targetLSN, err = c.getCurrentLSN()
	if err != nil {
		return fmt.Errorf("failed to get target LSN after WAL increment: %w", err)
	}
	log.Printf("Target LSN position to reach: %s", c.targetLSN.String())
	return nil
}

// getCurrentLSN retrieves the current LSN from the database using the standard connection.
func (c *PostgresChangeStreamConsumer) getCurrentLSN() (pglogrepl.LSN, error) {
	execCtx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	result := c.stdConn.ExecParams(execCtx, "SELECT pg_current_wal_lsn()::text", nil, nil, nil, nil).Read()
	if result.Err != nil {
		return 0, fmt.Errorf("failed to get current LSN: %w", result.Err)
	}

	if len(result.Rows) < 1 || len(result.Rows[0]) < 1 {
		return 0, fmt.Errorf("empty result from pg_current_wal_lsn()")
	}

	lsnText := string(result.Rows[0][0])
	lsn, err := pglogrepl.ParseLSN(lsnText)
	if err != nil {
		return 0, fmt.Errorf("failed to parse LSN '%s': %w", lsnText, err)
	}

	return lsn, nil
}

// forceWalIncrement sends a WAL message to force the WAL to advance using the standard connection.
func (c *PostgresChangeStreamConsumer) forceWalIncrement() error {
	execCtx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	// Use pg_logical_emit_message to emit a non-transactional message
	result := c.stdConn.ExecParams(execCtx, "SELECT pg_logical_emit_message(false, 'wal_advance', 'Force WAL increment')", nil, nil, nil, nil).Read()

	if result.Err != nil {
		return fmt.Errorf("failed to execute pg_logical_emit_message: %w", result.Err)
	}

	log.Printf("Successfully forced WAL increment using logical decoding message")
	return nil
}

// ConsumeToCompletion consumes the replication stream until the target LSN is reached or context cancelled.
func (c *PostgresChangeStreamConsumer) ConsumeToCompletion() (*seatbelt.DataFile, error) {
	// Determine the target LSN *now*, just before starting consumption
	if err := c.determineTargetLSN(); err != nil {
		// No need to call c.Close() here, as it will be called by the defer later
		return nil, fmt.Errorf("failed to determine target LSN before consumption: %w", err)
	}

	log.Printf("Starting replication consumer loop for table %s...", c.table.Name())

	// Split schema and table name for comparison
	schemaName, tableName := parseSchemaTable(c.table.Name())
	if tableName == "" { // If no schema, assume public or rely on search_path
		tableName = schemaName
		schemaName = "public" // Or get default schema? For now, assume public if not specified.
		log.Printf("Assuming public schema for table %s", tableName)
	}

	pluginArgs := []string{
		"proto_version '1'",
		fmt.Sprintf("publication_names '%s'", c.publicationName),
		"binary 'false'",  // Request text data
		"messages 'true'", // Receive logical decoding messages
	}
	err := pglogrepl.StartReplication(c.ctx, c.replConn, c.slotName, 0, pglogrepl.StartReplicationOptions{
		Mode:       pglogrepl.LogicalReplication,
		PluginArgs: pluginArgs,
	})
	if err != nil {
		// Specific error checking
		if strings.Contains(err.Error(), "does not exist") {
			if strings.Contains(err.Error(), "replication slot") {
				log.Printf("Replication slot '%s' does not exist. Please create it.", c.slotName)
			} else if strings.Contains(err.Error(), "publication") {
				log.Printf("Publication '%s' does not exist. Please create it.", c.publicationName)
			}
		}
		return nil, fmt.Errorf("failed to start replication stream: %w", err)
	}
	log.Printf("Successfully started replication stream with slot %s and publication %s", c.slotName, c.publicationName)

	clientXLogPos := pglogrepl.LSN(0)
	standbyMessageTimeout := time.Second * 10
	c.lastActivity = time.Now()

	idleCheckTicker := time.NewTicker(1 * time.Second)
	defer idleCheckTicker.Stop()

	defer c.Close() // Ensure connections are closed when function exits

	for {
		// Check context cancellation first
		if c.ctx.Err() != nil {
			log.Printf("Context cancelled, exiting replication loop: %v", c.ctx.Err())
			c.sendStandbyStatus(clientXLogPos) // Attempt final update
			return nil, c.ctx.Err()
		}

		// Check for idle timeout
		select {
		case <-idleCheckTicker.C:
			if c.idleTimeout > 0 && time.Since(c.lastActivity) > c.idleTimeout {
				log.Printf("No activity detected for %v, stopping replication consumer.", c.idleTimeout)
				c.sendStandbyStatus(clientXLogPos) // Attempt final update
				// We reached idle timeout, but this might be expected if target LSN isn't reached yet.
				// Should we error here or return the data collected so far?
				// The interface expects ConsumeToCompletion - let's assume idle means failure to reach target.
				// However, the original consumer returned results on idle. Let's stick to that for now.
				log.Println("Idle timeout reached. Returning collected results.")
				return c.createDataFile() // Return results collected so far
			}
		default:
			// non-blocking check
		}

		receiveTimeout := standbyMessageTimeout / 2
		if c.idleTimeout > 0 && c.idleTimeout < receiveTimeout {
			receiveTimeout = c.idleTimeout / 2 // Adjust receive timeout based on idle timeout
		}
		if receiveTimeout < 1*time.Second {
			receiveTimeout = 1 * time.Second // Ensure a minimum receive timeout
		}

		receiveCtx, receiveCancel := context.WithTimeout(c.ctx, receiveTimeout)
		msg, err := c.replConn.ReceiveMessage(receiveCtx)
		receiveCancel()

		if err != nil {
			if pgconn.Timeout(err) {
				// This is expected if no messages arrive within the timeout.
				// We checked idle timeout above, so just continue.
				continue
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				// Check if the main context was cancelled vs the receive timeout
				if c.ctx.Err() != nil {
					log.Printf("Context cancelled during ReceiveMessage, exiting loop: %v", c.ctx.Err())
					c.sendStandbyStatus(clientXLogPos)
					return nil, c.ctx.Err()
				}
				// Otherwise, it was just the receive timeout, continue the loop
				continue
			}
			var netErr net.Error
			if errors.As(err, &netErr) {
				log.Printf("Network error receiving message: %v", err)
				c.sendStandbyStatus(clientXLogPos)
				return nil, fmt.Errorf("network error receiving message: %w", err) // Fatal
			}

			// Other unexpected error
			log.Printf("Unexpected error receiving message: %v", err)
			c.sendStandbyStatus(clientXLogPos)
			return nil, fmt.Errorf("failed to receive replication message: %w", err)
		}

		// Update last activity time
		c.lastActivity = time.Now()

		switch msg := msg.(type) {
		case *pgproto3.CopyData:
			switch msg.Data[0] {
			case pglogrepl.PrimaryKeepaliveMessageByteID:
				// We don't need to do anything with keepalive, just acknowledges activity
				if c.debug {
					pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
					if err != nil {
						log.Printf("DEBUG: Error parsing keepalive: %v", err)
					} else {
						log.Printf("DEBUG: Received Keepalive: LSN %s, Timestamp %s, ReplyRequested %t", pkm.ServerWALEnd, pkm.ServerTime, pkm.ReplyRequested)
					}
				}

			case pglogrepl.XLogDataByteID:
				xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
				if err != nil {
					log.Printf("Error parsing XLogData: %v", err)
					continue // Skip this message
				}

				if c.debug {
					log.Printf("DEBUG: Received XLogData: WALStart %s, ServerWALEnd %s, ServerTime %s", xld.WALStart, xld.ServerWALEnd, xld.ServerTime)
				}

				logicalMsg, err := pglogrepl.Parse(xld.WALData)
				if err != nil {
					log.Printf("Error parsing logical replication message: %v", err)
					continue // Skip this message
				}

				switch logicalMsg := logicalMsg.(type) {
				case *pglogrepl.RelationMessage:
					c.relations[logicalMsg.RelationID] = logicalMsg
					if c.debug {
						log.Printf("DEBUG: Received Relation: ID=%d Schema=%s Table=%s Columns=%d", logicalMsg.RelationID, logicalMsg.Namespace, logicalMsg.RelationName, len(logicalMsg.Columns))
						// for i, col := range logicalMsg.Columns {
						// 	log.Printf("  Col %d: Name=%s, TypeID=%d", i, col.Name, col.DataTypeID)
						// }
					}

				case *pglogrepl.BeginMessage:
					if c.debug {
						log.Printf("DEBUG: Received Begin LSN: %s", logicalMsg.FinalLSN)
					}
				case *pglogrepl.CommitMessage:
					if c.debug {
						log.Printf("DEBUG: Received Commit LSN: %s", logicalMsg.CommitLSN)
					}

				case *pglogrepl.InsertMessage:
					c.handleDataMessage(logicalMsg.RelationID, logicalMsg.Tuple.Columns, schemaName, tableName)

				case *pglogrepl.UpdateMessage:
					c.handleDataMessage(logicalMsg.RelationID, logicalMsg.NewTuple.Columns, schemaName, tableName)

				case *pglogrepl.DeleteMessage:
					if c.debug {
						log.Printf("DEBUG: Ignoring DELETE for LSN")
					}

				case *pglogrepl.TruncateMessage:
					// Handle truncate if necessary (clear results for affected tables?)
					if c.debug {
						log.Printf("DEBUG: Ignoring TRUNCATE for %d relations", len(logicalMsg.RelationIDs))
					}

				default:
					log.Printf("Received unhandled logical message type: %T", logicalMsg)
				}

				// Important: Update the position we tell the server we've processed up to
				clientXLogPos = xld.WALStart + pglogrepl.LSN(len(xld.WALData))

				// Check if we've reached or passed the target LSN
				if c.targetLSN != 0 && clientXLogPos >= c.targetLSN {
					log.Printf("Reached target LSN %s (current: %s), completing replication",
						c.targetLSN.String(), clientXLogPos.String())
					c.sendStandbyStatus(clientXLogPos) // Send final status
					return c.createDataFile()          // Create and return the DataFile
				}
			}
		default:
			log.Printf("Received unexpected PostgreSQL message type: %T", msg)
		}
	}
}

// handleDataMessage processes Insert or Update messages for the configured table.
func (c *PostgresChangeStreamConsumer) handleDataMessage(relationID uint32, columns []*pglogrepl.TupleDataColumn, targetSchema, targetTable string) {
	rel, ok := c.relations[relationID]
	if !ok {
		log.Printf("Warning: Received data message for unknown relation ID: %d", relationID)
		return
	}

	// Check if this is the table we are interested in
	if rel.RelationName != targetTable || rel.Namespace != targetSchema {
		// if c.debug { log.Printf("DEBUG: Skipping message for relation %s.%s", rel.Namespace, rel.RelationName) }
		return
	}

	values := c.parseRow(rel, columns)
	pkColName := c.table.PrimaryKey()
	pkValRaw, pkOk := values[pkColName]
	if !pkOk {
		log.Printf("Error: Primary key column '%s' not found in message for %s.%s", pkColName, rel.Namespace, rel.RelationName)
		return
	}
	if pkValRaw == nil {
		log.Printf("Error: Primary key column '%s' is NULL in message for %s.%s", pkColName, rel.Namespace, rel.RelationName)
		return
	}

	// Assuming PK is text format from logical replication
	pkStr, ok := pkValRaw.(string)
	if !ok {
		log.Printf("Error: Could not decode primary key column '%s' (expected string): %T", pkColName, pkValRaw)
		return
	}

	values_array := make([]interface{}, len(c.table.SourceColumns()))
	for i, col := range c.table.SourceColumns() {
		values_array[i] = values[col.Name]
	}

	formatted_row_string, err := c.table.FormatSource(values_array)
	if err != nil {
		log.Printf("Error: Failed to format source row: %v", err)
		return
	}

	formatted_target_string, err := c.table.TransformSourceToCommon(values_array)
	if err != nil {
		log.Printf("Error: Failed to trasnform source row to common string: %v", err)
		return
	}

	hashPair := seatbelt.RowHashPair{
		SourceHash: c.table.SourceHash(formatted_row_string),
		TargetHash: c.table.TargetHash(formatted_target_string),
	}

	if c.debug {
		// log.Printf("DEBUG: Processed Row PK %s: SourceString='%s' -> SourceHash=%d",
		// 	pkStr, sourceConcatenatedString, computedSourceHash)
		// Log might need adjustment based on what MapAndHash does if we need intermediate string.
		log.Printf("DEBUG: Processed Row PK %s -> HashPair=%v", pkStr, hashPair)
	}

	// Store the result (overwrite if PK already exists)
	c.results[pkStr] = hashPair
}

// parseRow parses columns, expecting text format. Returns map[colName]string or nil for NULLs.
func (c *PostgresChangeStreamConsumer) parseRow(relation *pglogrepl.RelationMessage, columns []*pglogrepl.TupleDataColumn) map[string]interface{} {
	values := make(map[string]interface{})
	for idx, col := range columns {
		if idx >= len(relation.Columns) {
			log.Printf("Warning: Column index %d out of bounds for relation %s.%s (max %d)", idx, relation.Namespace, relation.RelationName, len(relation.Columns)-1)
			continue
		}
		colInfo := relation.Columns[idx]
		colName := colInfo.Name

		switch col.DataType {
		case 'n': // null
			values[colName] = nil
		case 'u': // unchanged toast - Treat as unavailable for hashing? Or does it mean use previous value? Treating as NULL for now.
			log.Printf("Warning: Column '%s' in %s.%s has UNCHANGED_TOAST value ('u'). Treating as NULL for hashing.", colName, relation.Namespace, relation.RelationName)
			values[colName] = nil
		case 't': // text
			// Data is the raw bytes of the text representation
			values[colName] = string(col.Data)
		case 'b': // binary - Should not happen with binary='false'
			log.Printf("Error: Received unexpected BINARY data ('b') for column '%s' in %s.%s despite requesting text format.", colName, relation.Namespace, relation.RelationName)
			// Attempt to treat as text, but this is likely wrong.
			values[colName] = string(col.Data) // Potentially corrupt data
		default:
			log.Printf("Warning: Unknown column data type '%c' for column '%s' in %s.%s", col.DataType, colName, relation.Namespace, relation.RelationName)
			values[colName] = nil // Treat unknown as null
		}
	}
	return values
}

// sendStandbyStatus sends a StandbyStatusUpdate message to the server.
func (c *PostgresChangeStreamConsumer) sendStandbyStatus(lsn pglogrepl.LSN) {
	standbyCtx, standbyCancel := context.WithTimeout(context.Background(), 5*time.Second) // Use background context for final update
	defer standbyCancel()
	err := pglogrepl.SendStandbyStatusUpdate(standbyCtx, c.replConn, pglogrepl.StandbyStatusUpdate{WALWritePosition: lsn})
	if err != nil {
		// Log errors, but don't make ConsumeToCompletion fail just because the final update failed.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			log.Printf("Warning: Timeout sending final standby status update (LSN %s)", lsn.String())
		} else {
			log.Printf("Warning: Error sending final standby status update (LSN %s): %v", lsn.String(), err)
		}
	} else {
		if c.debug {
			log.Printf("DEBUG: Sent StandbyStatusUpdate with LSN %s", lsn.String())
		}
	}
}

// createDataFile creates a temporary CSV file and writes the collected results to it.
func (c *PostgresChangeStreamConsumer) createDataFile() (*seatbelt.DataFile, error) {
	// Create a temporary file for the CSV data
	_, baseTableName := parseSchemaTable(c.table.Name())
	osfile, err := os.CreateTemp("", fmt.Sprintf("seatbelt-cdc-%s-*.csv", baseTableName))
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file for CDC results: %w", err)
	}
	file := seatbelt.NewDataFile(osfile)

	// Write header
	// Match Scan output format. Corrected unterminated string literal.
	header := fmt.Sprintf("%s,%s,%s\n", c.table.PrimaryKey(), "source_hash", "target_hash")
	if _, err := file.File.WriteString(header); err != nil { // Use file.File field
		file.Close() // Close file on error
		return nil, fmt.Errorf("failed to write header to CDC result file: %w", err)
	}

	// Write data rows
	rowCount := 0
	for pk, hashPair := range c.results {
		// TODO: Need proper CSV escaping if PK contains commas, quotes, or newlines
		// Corrected unterminated string literal.
		row := fmt.Sprintf("%s,%s,%s\n", pk, hashPair.SourceHash.String(), hashPair.TargetHash.String())
		if _, err := file.File.WriteString(row); err != nil { // Use file.File field
			file.Close() // Close file on error
			return nil, fmt.Errorf("failed to write row (PK: %s) to CDC result file: %w", pk, err)
		}
		rowCount++
	}

	file.SetRowCounter(int64(rowCount))

	// Rewind the file pointer so it can be read from the beginning
	if err := file.Rewind(); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to rewind CDC result file: %w", err)
	}

	log.Printf("Wrote %d rows to CDC result file: %s", rowCount, file.Name()) // Use file.Name() method
	return file, nil
}

// Close cleans up resources used by the consumer.
func (c *PostgresChangeStreamConsumer) Close() error {
	log.Println("Closing PostgresChangeStreamConsumer...")
	// Cancel the consumer's context to signal background operations
	if c.cancelCtx != nil {
		c.cancelCtx()
		c.cancelCtx = nil // Prevent double cancel
	}

	var firstErr error
	if c.replConn != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		log.Println("Closing replication connection...")
		err := c.replConn.Close(closeCtx)
		cancel()
		if err != nil {
			log.Printf("Error closing replication connection: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			log.Println("Replication connection closed.")
		}
		c.replConn = nil
	}
	if c.stdConn != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		log.Println("Closing standard connection...")
		err := c.stdConn.Close(closeCtx)
		cancel()
		if err != nil {
			log.Printf("Error closing standard connection: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			log.Println("Standard connection closed.")
		}
		c.stdConn = nil
	}
	log.Println("PostgresChangeStreamConsumer closed.")
	return firstErr
}

// parseSchemaTable splits a potentially schema-qualified table name.
func parseSchemaTable(fullName string) (schema, table string) {
	parts := strings.SplitN(fullName, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0] // Assume no schema if only one part
}
