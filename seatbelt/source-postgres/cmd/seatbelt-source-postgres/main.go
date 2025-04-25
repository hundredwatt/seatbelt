package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"seatbelt-source-postgres/pkg/config"
	"seatbelt-source-postgres/pkg/csvutil"
	"seatbelt-source-postgres/pkg/replication"
)

func main() {
	log.Println("Starting Seatbelt Batch Processor...")

	// --- Configuration ---
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load configuration from %s: %v", cfgPath, err)
	}

	log.Printf("Configuration loaded from %s", cfgPath)
	if cfg.Debug {
		log.Printf("DEBUG: Config = %+v", cfg)
	}
	log.Println("---")

	// Context for application lifecycle and shutdown signals
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
	poolCtx, poolCancel := context.WithTimeout(ctx, 15*time.Second)
	pool, err := pgxpool.New(poolCtx, cfg.Database.StdConnString)
	poolCancel()
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v", err)
	}
	defer pool.Close()
	log.Println("Standard database connection pool established.")
	log.Println("---")

	// --- Step 1: Fetch Hashes via SELECT Query ---
	selectHashes, err := fetchSelectHashes(ctx, pool, cfg)
	if err != nil {
		log.Fatalf("Failed to fetch hashes via SELECT: %v", err)
	}
	if len(selectHashes) == 0 {
		log.Println("Warning: Fetched 0 hashes via SELECT. Is the table populated?")
	}
	log.Println("---")

	// --- Step 2: Write SELECT Hashes to CSV ---
	err = csvutil.WriteIDHashMapToCSV(cfg.Output.SelectCSVPath, selectHashes, cfg.Table.IDColumn, "select_hash")
	if err != nil {
		log.Fatalf("Failed to write SELECT hashes to CSV '%s': %v", cfg.Output.SelectCSVPath, err)
	}
	log.Println("---")

	// --- Step 3: Process Replication Stream ---
	replConsumer, err := replication.NewReplicationConsumer(cfg)
	if err != nil {
		log.Fatalf("Failed to create replication consumer: %v", err)
	}
	defer func() {
		log.Println("Executing deferred replication consumer cleanup...")
		if cerr := replConsumer.Close(); cerr != nil {
			log.Printf("Error during deferred consumer close: %v", cerr)
		}
		log.Println("Replication consumer cleanup finished.")
	}()

	replHashes, err := replConsumer.Start(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		// Log error if it wasn't just a context cancellation
		log.Printf("Replication consumer stopped with error: %v", err)
	} else if errors.Is(err, context.Canceled) {
		log.Println("Replication consumer stopped due to signal.")
	} else {
		log.Println("Replication consumer finished normally (idle timeout). ")
	}
	log.Println("---")

	// --- Step 4: Write Replication Hashes to CSV ---
	// Only write if context wasn't cancelled prematurely (allow writing on normal idle exit)
	if ctx.Err() == nil || err == nil {
		err = csvutil.WriteIDHashMapToCSV(cfg.Output.ReplicationCSVPath, replHashes, cfg.Table.IDColumn, "replication_hash")
		if err != nil {
			log.Fatalf("Failed to write replication hashes to CSV '%s': %v", cfg.Output.ReplicationCSVPath, err)
		}
	} else {
		log.Printf("Skipping write of replication hashes to CSV due to shutdown signal.")
	}

	log.Println("---")
	log.Println("Seatbelt Batch Processor finished.")
}

// fetchSelectHashes executes the SELECT query to get IDs and computed hashes from the database.
func fetchSelectHashes(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) (map[int32]int64, error) {
	log.Printf("Fetching hashes via SELECT query for table %s...", cfg.Table.Name)

	// Build the concatenation part of the query dynamically
	var coalesceParts []string
	for _, colName := range cfg.Table.HashColumns {
		// Basic quoting to prevent SQL injection for column names (assuming valid identifiers)
		// A more robust solution might involve checking against actual table schema.
		safeColName := pgx.Identifier{colName}.Sanitize()
		coalesceParts = append(coalesceParts, fmt.Sprintf("COALESCE(%s::text, '')", safeColName))
	}
	concatenationExpression := strings.Join(coalesceParts, " || ")

	// Build the full query
	// Ensure ID column is also quoted safely
	safeIDColumn := pgx.Identifier{cfg.Table.IDColumn}.Sanitize()
	// Schema needs separate handling if used directly, but table name includes it
	// Split schema and table to sanitize properly
	var safeFullTableName string
	parts := strings.SplitN(cfg.Table.Name, ".", 2)
	if len(parts) == 2 {
		// We have schema.table format
		schema := pgx.Identifier{parts[0]}.Sanitize()
		table := pgx.Identifier{parts[1]}.Sanitize()
		safeFullTableName = schema + "." + table
	} else {
		// Just table name, assume public schema
		safeFullTableName = pgx.Identifier{cfg.Table.Name}.Sanitize()
	}

	// Note: Passing schema.table directly to Identifier might not work as expected for quoting.
	// Constructing schema.table quoting manually might be safer if needed.
	// For now, assuming cfg.Table.Name is correctly formatted e.g. `public.data_proof`

	query := fmt.Sprintf(`
		SELECT
			%s, -- ID Column
			hashtextextended((%s), $1) AS computed_hash
		FROM
			%s -- Table Name
	`, safeIDColumn, concatenationExpression, safeFullTableName)

	if cfg.Debug {
		log.Printf("DEBUG: Executing SQL: %s with seed: %d", query, cfg.HashSeed)
	}

	rows, err := pool.Query(ctx, query, cfg.HashSeed)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SELECT query: %w", err)
	}
	defer rows.Close()

	selectHashes := make(map[int32]int64)
	for rows.Next() {
		var id int32
		var hash pgtype.Int8 // Use pgtype to handle potential NULL hash if row exists but hash fails?
		if err := rows.Scan(&id, &hash); err != nil {
			log.Printf("Error scanning row for SELECT hash: %v", err)
			continue // Skip problematic rows
		}

		if hash.Valid {
			selectHashes[id] = hash.Int64
		} else {
			// This case implies hashtextextended returned NULL, which is unlikely unless input was NULL
			// and the function behavior changes, or the ID itself was NULL (which scan should handle).
			log.Printf("Warning: Received NULL computed hash for ID %d from SELECT query.", id)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating SELECT hash rows: %w", err)
	}

	log.Printf("Fetched %d hashes via SELECT.", len(selectHashes))
	return selectHashes, nil
}
