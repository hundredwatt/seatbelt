package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"seatbelt/pkg/config"
	"seatbelt/pkg/csvutil"
	"seatbelt/pkg/postgres"
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
	selectHashes, err := postgres.FetchSelectHashes(ctx, pool, cfg)
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
	replConsumer, err := postgres.NewReplicationConsumer(cfg)
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
