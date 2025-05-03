package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync" // Added for mutex and waitgroup
	"time"

	"seatbelt/pkg/seatbelt" // Use the core seatbelt types

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresChangeStreamConsumer implements seatbelt.ChangeStreamConsumer for PostgreSQL.
type PostgresChangeStreamConsumer struct {
	// Connections & Config
	replConn        *pgconn.PgConn
	stdConn         *pgconn.PgConn // Separate connection for non-replication tasks
	table           seatbelt.Table
	connString      string // Base connection string (non-replication)
	slotName        string // Replication slot name
	publicationName string // Publication name
	debug           bool
	idleTimeout     time.Duration

	// Internal State
	relations map[uint32]*pglogrepl.RelationMessage
	typeMap   *pgtype.Map

	// Processing Loop State
	ctx                 context.Context
	cancelCtx           context.CancelFunc
	mutex               sync.Mutex                      // Protects shared state below
	results             map[string]seatbelt.RowHashPair // Current batch of results
	batchCounter        int                             // Counter for current batch size
	dataFile            *seatbelt.DataFile              // Output file, created on first write
	lastActivity        time.Time                       // For idle timeout tracking
	clientXLogPos       pglogrepl.LSN                   // Last LSN processed by client
	targetLSN           pglogrepl.LSN                   // Target LSN to reach *after* completion requested
	completionRequested bool                            // Flag set by ConsumeToCompletion
	targetLSNReached    chan struct{}                   // Closed when target LSN is reached
	errorChan           chan error                      // Channel for errors from the loop goroutine
	loopWg              sync.WaitGroup                  // Waits for the replication loop goroutine
}

const defaultIdleTimeout = 10 * time.Second // Shorter default for testing
const defaultDebug = false
const defaultSlotName = "seatbelt_test_slot"       // Placeholder
const defaultPublicationName = "seatbelt_test_pub" // Placeholder
const resultBatchSize = 4096                       // Batch size for writing results

// NewPostgresChangeStreamConsumer creates a new consumer for a specific table and starts processing changes.
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
		// Connections & Config
		replConn:        replConn,
		stdConn:         stdConn,
		table:           table,
		connString:      connString,
		slotName:        slotName,
		publicationName: publicationName,
		debug:           debug,
		idleTimeout:     idleTimeout,

		// Internal State
		relations: make(map[uint32]*pglogrepl.RelationMessage),
		typeMap:   pgtype.NewMap(),

		// Processing Loop State
		ctx:              consumerCtx,
		cancelCtx:        consumerCancel,
		results:          make(map[string]seatbelt.RowHashPair), // Initialize results map for the first batch
		lastActivity:     time.Now(),
		targetLSNReached: make(chan struct{}), // Channel to signal completion
		errorChan:        make(chan error, 1), // Buffered channel for the loop error
	}

	// Start the replication loop in the background
	c.loopWg.Add(1)
	go c.runReplicationLoop()

	log.Printf("PostgresChangeStreamConsumer created for table %s and started processing.", table.Name())
	return c, nil
}

// runReplicationLoop is the main processing loop for the consumer.
// It runs in a separate goroutine and handles receiving and processing replication messages.
func (c *PostgresChangeStreamConsumer) runReplicationLoop() {
	defer c.loopWg.Done()
	defer func() {
		log.Println("Exiting replication loop.")
		// Ensure final standby status is sent if possible, using background context
		if c.replConn != nil {
			c.sendStandbyStatus(c.clientXLogPos) // Use the last known position
		}
	}()

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

	// Use c.ctx here for starting replication
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
		log.Printf("Error starting replication stream: %v", err)
		c.errorChan <- fmt.Errorf("failed to start replication stream: %w", err)
		return // Exit goroutine
	}
	log.Printf("Successfully started replication stream with slot %s and publication %s", c.slotName, c.publicationName)

	standbyMessageTimeout := time.Second * 10
	c.mutex.Lock()
	c.lastActivity = time.Now()
	c.mutex.Unlock()

	idleCheckTicker := time.NewTicker(1 * time.Second)
	defer idleCheckTicker.Stop()

	// No longer need defer c.Close() here, managed by the caller or main context

	for {
		// Check context cancellation first
		select {
		case <-c.ctx.Done():
			log.Printf("Context cancelled, exiting replication loop: %v", c.ctx.Err())
			c.errorChan <- c.ctx.Err() // Report context cancellation as error
			return                     // Exit loop
		default:
			// Continue processing
		}

		// Check for idle timeout
		select {
		case <-idleCheckTicker.C:
			c.mutex.Lock()
			idle := c.idleTimeout > 0 && time.Since(c.lastActivity) > c.idleTimeout
			c.mutex.Unlock()
			if idle {
				idleErr := fmt.Errorf("no activity detected for %v, stopping replication consumer", c.idleTimeout)
				log.Println(idleErr.Error())
				c.errorChan <- idleErr // Signal idle timeout as an error
				return                 // Exit loop
			}
		default:
			// non-blocking check
		}

		receiveTimeout := standbyMessageTimeout / 2
		c.mutex.Lock()
		if c.idleTimeout > 0 && c.idleTimeout < receiveTimeout {
			receiveTimeout = c.idleTimeout / 2 // Adjust receive timeout based on idle timeout
		}
		c.mutex.Unlock()
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
					c.errorChan <- c.ctx.Err()
					return // Exit loop
				}
				// Otherwise, it was just the receive timeout, continue the loop
				continue
			}
			var netErr net.Error
			if errors.As(err, &netErr) {
				netErrWrapped := fmt.Errorf("network error receiving message: %w", err)
				log.Println(netErrWrapped.Error())
				c.errorChan <- netErrWrapped // Fatal
				return                       // Exit loop
			}

			// Other unexpected error
			unexpectedErr := fmt.Errorf("failed to receive replication message: %w", err)
			log.Printf("Unexpected error receiving message: %v", err)
			c.errorChan <- unexpectedErr
			return // Exit loop
		}

		// Update last activity time
		c.mutex.Lock()
		c.lastActivity = time.Now()
		c.mutex.Unlock()

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

				// Update the client LSN position *before* checking for completion
				c.mutex.Lock()
				c.clientXLogPos = xld.WALStart + pglogrepl.LSN(len(xld.WALData))
				currentPos := c.clientXLogPos
				completionReq := c.completionRequested
				target := c.targetLSN
				c.mutex.Unlock()

				// Check if completion was requested AND we've reached/passed the target LSN
				if completionReq && target != 0 && currentPos >= target {
					log.Printf("Reached target LSN %s (current: %s), completing replication",
						target.String(), currentPos.String())
					c.mutex.Lock()
					close(c.targetLSNReached) // Signal that the target is reached
					// Prevent closing twice if loop continues briefly
					c.completionRequested = false // Reset flag after signalling
					c.mutex.Unlock()
					return // Exit the loop normally after signalling
				}
			}
		default:
			log.Printf("Received unexpected PostgreSQL message type: %T", msg)
		}
	}
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

// ConsumeToCompletion signals the consumer to stop after reaching a determined LSN and returns the collected data.
func (c *PostgresChangeStreamConsumer) ConsumeToCompletion() (*seatbelt.DataFile, error) {
	log.Println("ConsumeToCompletion called, preparing to finalize...")

	// 1. Signal the loop that completion is requested
	c.mutex.Lock()
	if c.completionRequested {
		c.mutex.Unlock()
		return nil, fmt.Errorf("ConsumeToCompletion already called") // Prevent concurrent calls
	}
	c.completionRequested = true
	c.mutex.Unlock()

	// 2. Determine the target LSN
	if err := c.determineTargetLSN(); err != nil {
		// If we can't determine target LSN, we can't gracefully complete.
		// Attempt to close, but return the error.
		log.Printf("Error determining target LSN: %v. Attempting shutdown.", err)
		_ = c.Close() // Best effort close
		return nil, fmt.Errorf("failed to determine target LSN for completion: %w", err)
	}
	log.Printf("Completion requested. Waiting for replication loop to reach target LSN: %s", c.targetLSN.String())

	// Set up a ticker to periodically send WAL messages to advance the LSN
	walAdvanceTicker := time.NewTicker(1 * time.Second)
	defer walAdvanceTicker.Stop()

	// 3. Wait for the loop to reach the target LSN or exit with an error
	for {
		select {
		case <-c.targetLSNReached:
			log.Printf("Replication loop confirmed target LSN %s reached.", c.targetLSN.String())
			// Proceed to final write
			goto finalizeData
		case err := <-c.errorChan:
			log.Printf("Replication loop exited with error while waiting for LSN: %v", err)
			_ = c.Close() // Best effort close
			return nil, fmt.Errorf("replication loop failed during completion: %w", err)
		case <-c.ctx.Done():
			log.Printf("Context cancelled while waiting for target LSN: %v", c.ctx.Err())
			_ = c.Close() // Best effort close
			return nil, fmt.Errorf("context cancelled during completion: %w", c.ctx.Err())
		case <-walAdvanceTicker.C:
			// Force WAL advancement to help reach target LSN
			log.Printf("Sending WAL message to advance LSN toward target %s", c.targetLSN.String())
			if err := c.forceWalIncrement(); err != nil {
				log.Printf("Error forcing WAL increment: %v. Will retry.", err)
			}
		}
	}

finalizeData:
	// 4. Write the final batch of results
	c.mutex.Lock()
	defer c.mutex.Unlock()

	log.Println("Writing final batch of results...")
	if err := c.writeCurrentBatch(); err != nil {
		// Log critical error, but try to proceed with what we have.
		log.Printf("CRITICAL: Failed to write final result batch: %v.", err)
		// Don't return error here, try to return the file anyway if it exists.
	}

	// 5. Ensure the data file exists, even if empty
	if c.dataFile == nil {
		log.Println("No data file was created (likely no rows processed). Creating empty file.")
		if err := c.openDataFile(); err != nil {
			// If we can't even create an empty file, something is wrong.
			log.Printf("Failed to create empty data file: %v", err)
			// Don't call Close() here as mutex is held
			return nil, fmt.Errorf("failed to create final data file: %w", err)
		}
	}

	// 6. Rewind the file and prepare for return
	if err := c.dataFile.Rewind(); err != nil {
		log.Printf("Failed to rewind final data file %s: %v", c.dataFile.Name(), err)
		// Attempt to close the file before returning error
		_ = c.dataFile.Close()
		c.dataFile = nil // Prevent double close in Close() method
		return nil, fmt.Errorf("failed to rewind final data file: %w", err)
	}

	resultFile := c.dataFile
	c.dataFile = nil // Transfer ownership, prevent Close() from closing it.

	log.Printf("ConsumeToCompletion finished successfully. Returning data file: %s (%d rows)", resultFile.Name(), resultFile.RowCount())
	return resultFile, nil
}

// openDataFile creates the temporary data file if it doesn't exist and writes the header.
// The mutex must be held by the caller.
func (c *PostgresChangeStreamConsumer) openDataFile() error {
	if c.dataFile != nil {
		return nil // Already open
	}

	_, baseTableName := parseSchemaTable(c.table.Name())
	osfile, err := os.CreateTemp("", fmt.Sprintf("seatbelt-cdc-%s-*.csv", baseTableName))
	if err != nil {
		return fmt.Errorf("failed to create temp file for CDC results: %w", err)
	}

	dataFile := seatbelt.NewDataFile(osfile)

	// Write header
	header := fmt.Sprintf("%s,%s,%s\n", "pk", "source_hash", "target_hash")
	if _, err := dataFile.File.WriteString(header); err != nil {
		dataFile.Close() // Close file on error
		return fmt.Errorf("failed to write header to CDC result file: %w", err)
	}

	c.dataFile = dataFile
	log.Printf("Opened CDC result file: %s", c.dataFile.Name())
	return nil
}

// writeCurrentBatch writes the currently accumulated results (c.results) to the data file.
// The mutex must be held by the caller.
func (c *PostgresChangeStreamConsumer) writeCurrentBatch() error {
	if len(c.results) == 0 {
		return nil // Nothing to write
	}

	// Ensure data file is open
	if err := c.openDataFile(); err != nil {
		// Don't wipe results if file opening failed, maybe retry later?
		// For now, just return the error.
		return fmt.Errorf("failed to open data file for writing batch: %w", err)
	}

	batchRowCount := 0
	for pk, hashPair := range c.results {
		// TODO: Need proper CSV escaping if PK contains commas, quotes, or newlines
		row := fmt.Sprintf("%s,%s,%s\n", pk, hashPair.SourceHash.String(), hashPair.TargetHash.String())
		if _, err := c.dataFile.File.WriteString(row); err != nil {
			// Don't close the file here, let the main Close handle it.
			// Return an error indicating which row failed.
			return fmt.Errorf("failed to write row (PK: %s) to CDC result file %s: %w", pk, c.dataFile.Name(), err)
		}
		batchRowCount++
	}

	// Update the total row count in the DataFile object
	newRowCount := c.dataFile.RowCount() + int64(batchRowCount) // Calculate new total
	c.dataFile.SetRowCounter(newRowCount)                       // Set the new total count

	if c.debug {
		log.Printf("DEBUG: Wrote batch of %d rows to %s (Total rows: %d)", batchRowCount, c.dataFile.Name(), c.dataFile.RowCount())
	}

	return nil
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
		log.Printf("DEBUG: Processed Row PK %s -> HashPair=%v", pkStr, hashPair)
	}

	// Acquire mutex before accessing shared state (results, batchCounter, dataFile)
	c.mutex.Lock()
	defer c.mutex.Unlock() // Ensure mutex is released

	// Store the result (overwrite if PK already exists)
	c.results[pkStr] = hashPair
	c.batchCounter++

	// Check if batch is full
	if c.batchCounter >= resultBatchSize {
		if c.debug {
			log.Printf("DEBUG: Batch size %d reached, writing batch.", resultBatchSize)
		}
		if err := c.writeCurrentBatch(); err != nil {
			log.Printf("CRITICAL: Failed to write result batch: %v. Results for this batch may be lost.", err)
			// If writing fails, we still reset the batch to prevent repeated failures
			// on the same data. Error is logged.
		}
		// Reset batch
		c.results = make(map[string]seatbelt.RowHashPair) // Start new batch
		c.batchCounter = 0
	}
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

// Close cleans up resources used by the consumer.
func (c *PostgresChangeStreamConsumer) Close() error {
	log.Println("Closing PostgresChangeStreamConsumer...")

	// 1. Cancel the context to signal the loop and other operations
	c.mutex.Lock()
	if c.cancelCtx != nil {
		log.Println("Cancelling consumer context...")
		c.cancelCtx()
		c.cancelCtx = nil // Prevent double cancel
	}
	// Check if completion already happened and file was returned
	dataFileNeedsClose := c.dataFile != nil
	c.mutex.Unlock() // Unlock before waiting

	// 2. Wait for the replication loop goroutine to finish
	log.Println("Waiting for replication loop to exit...")
	c.loopWg.Wait()
	log.Println("Replication loop finished.")

	// 3. Close connections
	var firstErr error
	if c.replConn != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		log.Println("Closing replication connection...")
		err := c.replConn.Close(closeCtx)
		cancel()
		if err != nil {
			log.Printf("Error closing replication connection: %v", err)
			if firstErr == nil {
				firstErr = fmt.Errorf("error closing replication connection: %w", err)
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
				firstErr = fmt.Errorf("error closing standard connection: %w", err)
			}
		} else {
			log.Println("Standard connection closed.")
		}
		c.stdConn = nil
	}

	// 4. Close the data file if it wasn't returned by ConsumeToCompletion
	c.mutex.Lock()
	defer c.mutex.Unlock()                       // Lock for the remainder of the function
	if dataFileNeedsClose && c.dataFile != nil { // Double check dataFile hasn't become nil
		log.Printf("Closing data file %s as it was not returned by ConsumeToCompletion.", c.dataFile.Name())
		if err := c.dataFile.Close(); err != nil {
			log.Printf("Error closing data file %s: %v", c.dataFile.Name(), err)
			if firstErr == nil {
				firstErr = fmt.Errorf("error closing data file %s: %w", c.dataFile.Name(), err)
			}
		} else {
			log.Printf("Data file %s closed.", c.dataFile.Name())
		}
		c.dataFile = nil
	}

	// Clean up channels just in case (though loop should handle targetLSNReached)
	if c.targetLSNReached != nil {
		close(c.targetLSNReached) // Safe to close already closed channel
		c.targetLSNReached = nil
	}
	if c.errorChan != nil {
		close(c.errorChan)
		c.errorChan = nil
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
