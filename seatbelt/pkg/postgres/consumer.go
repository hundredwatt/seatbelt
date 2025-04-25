package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"seatbelt/pkg/config" // Correct path relative to module root
	// Correct path relative to module root
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
)

// ReplicationResult holds the processed data from the replication stream.
type ReplicationResult struct {
	ID   int32
	Hash int64
}

// ReplicationConsumer manages the replication connection and message handling
type ReplicationConsumer struct {
	conn         *pgconn.PgConn
	cfg          *config.Config
	relations    map[uint32]*pglogrepl.RelationMessage
	typeMap      *pgtype.Map
	idleTimeout  time.Duration
	lastActivity time.Time
	results      map[int32]int64 // Store results here (ID -> Hash)
	debug        bool
	targetLSN    pglogrepl.LSN // Target LSN to reach before completing
}

// NewReplicationConsumer creates a new consumer
func NewReplicationConsumer(cfg *config.Config) (*ReplicationConsumer, error) {
	// Use a separate context for connection as it's short-lived
	connCtx, connCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer connCancel()

	conn, err := pgconn.Connect(connCtx, cfg.Database.ReplConnString)
	if err != nil {
		return nil, fmt.Errorf("failed to connect replication connection: %w", err)
	}

	// Override the idle timeout to 12 hours if specified by the user
	idleTimeout := cfg.IdleTimeout
	if idleTimeout <= 10*time.Second {
		idleTimeout = 12 * time.Hour
		log.Printf("Setting idle timeout to 12 hours")
	}

	return &ReplicationConsumer{
		conn:         conn,
		cfg:          cfg,
		relations:    make(map[uint32]*pglogrepl.RelationMessage),
		typeMap:      pgtype.NewMap(),
		idleTimeout:  idleTimeout,
		lastActivity: time.Now(),
		results:      make(map[int32]int64),
		debug:        cfg.Debug,
	}, nil
}

// Start consumes the replication stream until idle timeout or context cancellation.
func (c *ReplicationConsumer) Start(ctx context.Context) (map[int32]int64, error) {
	log.Printf("Starting replication consumer loop for table %s...", c.cfg.Table.Name)

	// Create a standard connection for operations that can't use replication connection
	stdConn, err := c.createStandardConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create standard connection: %w", err)
	}
	defer stdConn.Close(context.Background())

	// First, get the current LSN from the database using standard connection
	currentLSN, err := c.getCurrentLSN(ctx, stdConn)
	if err != nil {
		return nil, fmt.Errorf("failed to get current LSN: %w", err)
	}
	log.Printf("Current database LSN position: %s", currentLSN.String())

	// Next, send a WAL message to force LSN increment using standard connection
	err = c.forceWalIncrement(ctx, stdConn)
	if err != nil {
		return nil, fmt.Errorf("failed to force WAL increment: %w", err)
	}

	// Get the new target LSN after the WAL increment using standard connection
	c.targetLSN, err = c.getCurrentLSN(ctx, stdConn)
	if err != nil {
		return nil, fmt.Errorf("failed to get target LSN after WAL increment: %w", err)
	}
	log.Printf("Target LSN position to reach: %s", c.targetLSN.String())

	pluginArgs := []string{
		"proto_version '1'",
		fmt.Sprintf("publication_names '%s'", c.cfg.Replication.PublicationName),
		"binary 'false'",  // Request text data for easier processing
		"messages 'true'", // Receive messages
	}
	err = pglogrepl.StartReplication(ctx, c.conn, c.cfg.Replication.SlotName, 0, pglogrepl.StartReplicationOptions{
		Mode:       pglogrepl.LogicalReplication,
		PluginArgs: pluginArgs,
	})
	if err != nil {
		// Specific error checking for common issues
		if strings.Contains(err.Error(), "does not exist") {
			log.Printf("Replication slot '%s' does not exist. Please create it.", c.cfg.Replication.SlotName)
		} else if strings.Contains(err.Error(), "publication") && strings.Contains(err.Error(), "does not exist") {
			log.Printf("Publication '%s' does not exist. Please create it.", c.cfg.Replication.PublicationName)
		}
		return nil, fmt.Errorf("failed to start replication stream: %w", err)
	}
	log.Printf("Successfully started replication stream with slot %s and publication %s", c.cfg.Replication.SlotName, c.cfg.Replication.PublicationName)

	clientXLogPos := pglogrepl.LSN(0)
	standbyMessageTimeout := time.Second * 10
	c.lastActivity = time.Now()

	// Create a timer to check for idle timeout
	idleCheckTicker := time.NewTicker(1 * time.Second)
	defer idleCheckTicker.Stop()

	for {
		// Check context cancellation first
		if ctx.Err() != nil {
			log.Printf("Context cancelled, exiting replication loop: %v", ctx.Err())

			// Send final standby status update before returning
			standbyCtx, standbyCancel := context.WithTimeout(context.Background(), standbyMessageTimeout)
			err := pglogrepl.SendStandbyStatusUpdate(standbyCtx, c.conn, pglogrepl.StandbyStatusUpdate{WALWritePosition: clientXLogPos})
			standbyCancel()
			if err != nil {
				log.Printf("Error sending final standby status update: %v", err)
			} else {
				log.Println("Sent final standby status update on context cancellation")
			}

			return c.results, ctx.Err() // Return results collected so far
		}

		// Check for idle timeout independently from message receiving
		select {
		case <-idleCheckTicker.C:
			if c.idleTimeout > 0 && time.Since(c.lastActivity) > c.idleTimeout {
				log.Printf("No activity detected for %v, stopping replication consumer.", c.idleTimeout)

				// Send final standby status update before returning
				standbyCtx, standbyCancel := context.WithTimeout(context.Background(), standbyMessageTimeout)
				err := pglogrepl.SendStandbyStatusUpdate(standbyCtx, c.conn, pglogrepl.StandbyStatusUpdate{WALWritePosition: clientXLogPos})
				standbyCancel()
				if err != nil {
					log.Printf("Error sending final standby status update: %v", err)
				} else {
					log.Println("Sent final standby status update on idle timeout")
				}

				return c.results, nil // Normal exit on idle
			}
		default:
			// non-blocking check, continue
		}

		// Use a receive timeout that's reasonably responsive
		receiveTimeout := standbyMessageTimeout / 2
		if c.idleTimeout > 0 && c.idleTimeout < receiveTimeout {
			receiveTimeout = c.idleTimeout / 2
		}

		receiveCtx, receiveCancel := context.WithTimeout(ctx, receiveTimeout)
		msg, err := c.conn.ReceiveMessage(receiveCtx)
		receiveCancel()

		if err != nil {
			if pgconn.Timeout(err) {
				// Check idle timeout again after timeout
				if c.idleTimeout > 0 && time.Since(c.lastActivity) > c.idleTimeout {
					log.Printf("No activity detected for %v after receive timeout, stopping replication consumer.", c.idleTimeout)

					// Send final standby status update before returning
					standbyCtx, standbyCancel := context.WithTimeout(context.Background(), standbyMessageTimeout)
					err := pglogrepl.SendStandbyStatusUpdate(standbyCtx, c.conn, pglogrepl.StandbyStatusUpdate{WALWritePosition: clientXLogPos})
					standbyCancel()
					if err != nil {
						log.Printf("Error sending final standby status update: %v", err)
					} else {
						log.Println("Sent final standby status update on receive timeout")
					}

					return c.results, nil
				}
				continue // Normal timeout, loop again
			}
			if ctx.Err() != nil {
				log.Printf("Context cancelled during or after ReceiveMessage, exiting loop: %v", ctx.Err())

				// Send final standby status update before returning
				standbyCtx, standbyCancel := context.WithTimeout(context.Background(), standbyMessageTimeout)
				err := pglogrepl.SendStandbyStatusUpdate(standbyCtx, c.conn, pglogrepl.StandbyStatusUpdate{WALWritePosition: clientXLogPos})
				standbyCancel()
				if err != nil {
					log.Printf("Error sending final standby status update: %v", err)
				} else {
					log.Println("Sent final standby status update on context cancellation after receive")
				}

				return c.results, ctx.Err()
			}
			var netErr net.Error
			if errors.As(err, &netErr) {
				log.Printf("Network error receiving message: %v", err)

				// Send final standby status update before returning
				standbyCtx, standbyCancel := context.WithTimeout(context.Background(), standbyMessageTimeout)
				err := pglogrepl.SendStandbyStatusUpdate(standbyCtx, c.conn, pglogrepl.StandbyStatusUpdate{WALWritePosition: clientXLogPos})
				standbyCancel()
				if err != nil {
					log.Printf("Error sending final standby status update: %v", err)
				} else {
					log.Println("Sent final standby status update on network error")
				}

				return c.results, fmt.Errorf("network error receiving message: %w", err) // Likely fatal
			}
			// Other receive error

			// Send final standby status update before returning
			standbyCtx, standbyCancel := context.WithTimeout(context.Background(), standbyMessageTimeout)
			err2 := pglogrepl.SendStandbyStatusUpdate(standbyCtx, c.conn, pglogrepl.StandbyStatusUpdate{WALWritePosition: clientXLogPos})
			standbyCancel()
			if err2 != nil {
				log.Printf("Error sending final standby status update: %v", err2)
			} else {
				log.Println("Sent final standby status update on receive error")
			}

			return c.results, fmt.Errorf("failed to receive replication message: %w", err)
		}

		// Update last activity time whenever any message is received
		c.lastActivity = time.Now()

		switch msg := msg.(type) {
		case *pgproto3.CopyData:
			switch msg.Data[0] {
			case pglogrepl.PrimaryKeepaliveMessageByteID:
				// We don't need to use pkm except for debugging
				_, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
				if err != nil {
					log.Printf("Failed to parse keepalive message: %v", err)
					continue // Log and continue
				}
				// We ignore ReplyRequested since we're only sending updates at the end
				// But we should still update activity time (done above)

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
					if c.debug {
						log.Printf("DEBUG: Received Relation: ID=%d Schema=%s Table=%s", logicalMsg.RelationID, logicalMsg.Namespace, logicalMsg.RelationName)
					}

				case *pglogrepl.BeginMessage:
				case *pglogrepl.CommitMessage:

				case *pglogrepl.InsertMessage:
					c.handleDataMessage(logicalMsg.RelationID, logicalMsg.Tuple.Columns)

				case *pglogrepl.UpdateMessage:
					// For updates, the new tuple contains the full row image
					c.handleDataMessage(logicalMsg.RelationID, logicalMsg.NewTuple.Columns)

				case *pglogrepl.DeleteMessage:
					// We might want to remove the ID from our results if deleted?
					// For now, we only care about INSERT/UPDATE for hashing state.
					// c.handleDataMessage(logicalMsg.RelationID, logicalMsg.OldTuple.Columns, true) // Handle delete if needed
					if c.debug {
						rel, _ := c.relations[logicalMsg.RelationID]
						log.Printf("DEBUG: Ignoring DELETE for RelationID=%d (%s.%s)", logicalMsg.RelationID, rel.Namespace, rel.RelationName)
					}

				case *pglogrepl.TruncateMessage:
					// Handle truncate if necessary (clear results?)
					if c.debug {
						log.Printf("DEBUG: Ignoring TRUNCATE")
					}

				default:
					log.Printf("Received unhandled logical message type: %T", logicalMsg)
				}

				// Update LSN position
				clientXLogPos = xld.WALStart + pglogrepl.LSN(len(xld.WALData))

				// Check if we've reached or exceeded our target LSN
				if clientXLogPos >= c.targetLSN {
					log.Printf("Reached target LSN %s (current: %s), completing replication",
						c.targetLSN.String(), clientXLogPos.String())

					// Send final standby status update before returning
					standbyCtx, standbyCancel := context.WithTimeout(context.Background(), standbyMessageTimeout)
					err := pglogrepl.SendStandbyStatusUpdate(standbyCtx, c.conn, pglogrepl.StandbyStatusUpdate{WALWritePosition: clientXLogPos})
					standbyCancel()
					if err != nil {
						log.Printf("Error sending final standby status update: %v", err)
					} else {
						log.Println("Sent final standby status update after reaching target LSN")
					}

					return c.results, nil
				}
			}
		default:
			log.Printf("Received unexpected physical message type: %T", msg)
		}
	}
}

// createStandardConnection creates a standard (non-replication) connection using the same parameters
func (c *ReplicationConsumer) createStandardConnection(ctx context.Context) (*pgconn.PgConn, error) {
	// Create a standard connection string from the replication one by removing replication=database
	stdConnString := c.cfg.Database.StdConnString
	if stdConnString == "" {
		// If for some reason StdConnString is not set, derive it from the replication connection string
		stdConnString = strings.Replace(c.cfg.Database.ReplConnString, "replication=database", "", 1)
		stdConnString = strings.TrimRight(stdConnString, "?&")
	}

	connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
	defer connCancel()

	conn, err := pgconn.Connect(connCtx, stdConnString)
	if err != nil {
		return nil, fmt.Errorf("failed to create standard connection: %w", err)
	}

	return conn, nil
}

// getCurrentLSN retrieves the current LSN from the database using standard connection
func (c *ReplicationConsumer) getCurrentLSN(ctx context.Context, conn *pgconn.PgConn) (pglogrepl.LSN, error) {
	result := conn.ExecParams(ctx, "SELECT pg_current_wal_lsn()::text", nil, nil, nil, nil).Read()
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

// forceWalIncrement sends a WAL message to force the WAL to advance using standard connection
func (c *ReplicationConsumer) forceWalIncrement(ctx context.Context, conn *pgconn.PgConn) error {
	// Use pg_logical_emit_message to emit a non-transactional message
	// This will force WAL to advance without needing to create tables
	result := conn.ExecParams(ctx, "SELECT pg_logical_emit_message(false, 'wal_advance', 'Force WAL increment')", nil, nil, nil, nil).Read()

	if result.Err != nil {
		return fmt.Errorf("failed to execute pg_logical_emit_message: %w", result.Err)
	}

	log.Printf("Successfully forced WAL increment using logical decoding message")
	return nil
}

// handleDataMessage processes Insert or Update messages for the configured table.
func (c *ReplicationConsumer) handleDataMessage(relationID uint32, columns []*pglogrepl.TupleDataColumn) {
	rel, ok := c.relations[relationID]
	if !ok {
		log.Printf("Warning: Received data message for unknown relation ID: %d", relationID)
		return
	}

	// Check if this is the table we are interested in
	if rel.RelationName != c.cfg.TableName || rel.Namespace != c.cfg.SchemaName {
		// log.Printf("DEBUG: Skipping message for relation %s.%s", rel.Namespace, rel.RelationName)
		return
	}

	values := c.parseRow(rel, columns)
	idVal, idOk := values[c.cfg.Table.IDColumn]
	if !idOk || idVal == nil {
		log.Printf("Error: Missing or NULL ID column '%s' in relation %s.%s: %+v", c.cfg.Table.IDColumn, rel.Namespace, rel.RelationName, values)
		return
	}

	// Decode the ID (expected text format)
	var id int32
	if idBytes, typeOk := idVal.([]byte); typeOk {
		idStr := string(idBytes)
		parsedID, err := strconv.ParseInt(idStr, 10, 32) // Parse as base-10, 32-bit int
		if err != nil {
			log.Printf("Error: Could not parse ID column '%s' from text '%s': %v", c.cfg.Table.IDColumn, idStr, err)
			return
		}
		id = int32(parsedID)
	} else {
		log.Printf("Error: Could not decode ID column '%s' (expected text/bytes): %T %+v", c.cfg.Table.IDColumn, idVal, idVal)
		return
	}

	// Concatenate configured hash columns
	var builder strings.Builder
	concatenatedString := ""
	for _, colName := range c.cfg.Table.HashColumns {
		val, ok := values[colName]
		if !ok {
			log.Printf("Error: Missing hash column '%s' for ID %d in %s.%s. Cannot compute hash.", colName, id, rel.Namespace, rel.RelationName)
			return // Cannot compute hash if a required column is missing
		}
		if val == nil {
			builder.WriteString("👻") // Use ghost emoju for NULL
		} else if bytesVal, typeOk := val.([]byte); typeOk {
			builder.WriteString(string(bytesVal)) // Append text representation
		} else {
			log.Printf("Error: Unexpected type for hash column '%s' (ID %d): %T. Treating as empty string.", colName, id, val)
			builder.WriteString("")
		}
	}
	concatenatedString = builder.String()

	// Compute hash
	computedHash := PostgresHashtextextend(concatenatedString, c.cfg.HashSeed)

	if c.debug {
		log.Printf("DEBUG: Processed Row ID %d: String='%s' -> Hash=%d", id, concatenatedString, computedHash)
	}

	// Store the result (overwrite if ID already exists)
	c.results[id] = computedHash
}

// parseRow parses columns, expecting text format.
func (c *ReplicationConsumer) parseRow(relation *pglogrepl.RelationMessage, columns []*pglogrepl.TupleDataColumn) map[string]interface{} {
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
		case 'u': // unchanged toast
			log.Printf("Warning: Column '%s' has UNCHANGED_TOAST value ('u'), treating as NULL.", colName)
			values[colName] = nil
		case 't': // text
			values[colName] = col.Data // Store raw bytes
		case 'b': // binary - Should ideally not happen for hash columns if binary=false
			log.Printf("Warning: Received unexpected BINARY data ('b') for column '%s'. Attempting text conversion.", colName)
			// Try to treat as text; might fail depending on actual binary content
			values[colName] = col.Data
		default:
			log.Printf("Warning: Unknown column data type '%c' for column '%s'", col.DataType, colName)
			values[colName] = nil // Treat unknown as null
		}
	}
	return values
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
