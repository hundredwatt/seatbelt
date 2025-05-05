package main

import (
	"context"
	"database/sql" // Import standard SQL package for ClickHouse
	"fmt"
	"log"
	"os"
	"time"

	"seatbelt/pkg/clickhouse"
	"seatbelt/pkg/postgres"
	"seatbelt/pkg/seatbelt" // Assuming seatbelt core logic is here

	// "seatbelt/pkg/sources" // Assuming source implementations are here (e.g., postgres)
	// "seatbelt/pkg/targets" // Assuming target implementations are here (e.g., clickhouse)
	"seatbelt/pkg/row_mappers" // Import row mappers package

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	// Import necessary DB drivers and packages
	_ "github.com/ClickHouse/clickhouse-go/v2" // ClickHouse driver
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds the application configuration loaded from YAML
type AppConfig struct {
	SourceConnectionString string                   `yaml:"source_connection_string"`
	TargetConnectionString string                   `yaml:"target_connection_string"`
	RowMapperName          string                   `yaml:"row_mapper_name"`
	TableName              string                   `yaml:"table_name"`
	TargetTableName        string                   `yaml:"target_table_name"` // Optional
	PrimaryKeyName         string                   `yaml:"primary_key_name"`
	Columns                []seatbelt.ColumnMapping `yaml:"columns"`
	ShadowPath             string                   `yaml:"seatbelt_data_path"` // Optional
	Environment            map[string]string        `yaml:"environment"`        // Environment variables
}

var (
	configFile        string
	fetchDataOnly     bool
	sourceScanFile    string
	targetScanFile    string
	sourceChangesFile string
	explainAnalyze    bool
	initialLoad       bool
)

var rootCmd = &cobra.Command{
	Use:   "seatbelt",
	Short: "Seatbelt is a tool for data validation between sources and targets",
	Long:  `Seatbelt helps ensure data consistency by comparing data between a source and a target system using cryptographic hashes and maintaining a shadow table.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// You can add global setup here if needed
	},
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the full Seatbelt process (fetch data and update shadow)",
	Long:  `Loads configuration, fetches data from source and target, updates the shadow table, and prints validation metrics.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := loadConfig(configFile)
		if err != nil {
			log.Fatalf("Error loading config file '%s': %v", configFile, err)
		}

		ctx := context.Background()

		// 1. Create Components (Source, Target, Table, RowMapper)
		source, target, table, sourceCleanup, targetCleanup, err := createComponents(ctx, cfg)
		if err != nil {
			log.Fatalf("Error creating components: %v", err)
		}
		// Defer cleanup functions to close connections when done
		defer sourceCleanup()
		defer targetCleanup()

		seatbeltCfg := &seatbelt.Config{
			Table:       table,
			Source:      source,
			Target:      target,
			ShadowPath:  cfg.ShadowPath, // Pass shadow path from AppConfig
			InitialLoad: initialLoad,    // Set initial load flag from command line
		}

		// 2. Fetch Data
		fmt.Println("Fetching data...")
		dataFiles, err := seatbelt.FetchData(ctx, seatbeltCfg)
		if err != nil {
			log.Fatalf("Error fetching data: %v", err)
		}
		fmt.Println("Data fetched successfully.")
		if !initialLoad {
			fmt.Printf("  Source Scan: %s (%d rows)\n", dataFiles.SourceScan.Name(), dataFiles.SourceScan.RowCount())
		}
		fmt.Printf("  Target Scan: %s (%d rows)\n", dataFiles.TargetScan.Name(), dataFiles.TargetScan.RowCount())
		if !initialLoad && dataFiles.SourceChanges != nil {
			fmt.Printf("  Source Changes: %s (%d rows)\n", dataFiles.SourceChanges.Name(), dataFiles.SourceChanges.RowCount())
		}
		if dataFiles.SourceExtractScan != nil {
			fmt.Printf("  Source Extract Scan: %s (%d rows)\n", dataFiles.SourceExtractScan.Name(), dataFiles.SourceExtractScan.RowCount())
		}

		if fetchDataOnly {
			fmt.Println("Fetch data only mode enabled. Skipping shadow update.")
			return // Exit early as requested
		}

		// 3. Update Shadow
		fmt.Println("Updating shadow table...")
		metrics, err := seatbelt.UpdateShadow(ctx, seatbeltCfg, dataFiles)
		if err != nil {
			log.Fatalf("Error updating shadow table: %v", err)
		}
		fmt.Println("Shadow table updated successfully.")

		// 4. Print Validation Metrics
		printMetrics(metrics)
	},
}

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch data from source and target without updating the shadow table",
	Long:  `Loads configuration and fetches data from source and target, saving the results to temporary files.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Essentially the same as runCmd but forces fetchDataOnly = true
		fetchDataOnly = true  // Force fetch only mode for this command
		runCmd.Run(cmd, args) // Reuse runCmd logic
	},
}

var shadowCmd = &cobra.Command{
	Use:   "shadow",
	Short: "Update the shadow table using pre-existing data files",
	Long:  `Updates the shadow table using data files provided via flags, skipping the data fetch phase. Allows optional EXPLAIN ANALYZE.`,
	Run: func(cmd *cobra.Command, args []string) {
		if sourceScanFile == "" || targetScanFile == "" || sourceChangesFile == "" {
			log.Fatal("Source scan, target scan, and source changes file paths must be provided for shadow update.")
		}

		// Minimal config needed for UpdateShadow when files are provided
		seatbeltCfg := &seatbelt.Config{
			ShadowPath: "", // Shadow path might be specified via flag if needed, or use default
			// Table, Source, Target are not strictly needed if we just call UpdateShadow
			// but UpdateShadow currently takes the full config. We might need to refactor
			// UpdateShadow or pass dummy components. For now, let's create a temporary config.
		}
		// ShadowPath might need a flag as well if not using :memory:
		// explainAnalyze flag is handled within UpdateShadow (needs implementation)

		ctx := context.Background()

		// Open the provided files
		sourceScanF, err := os.OpenFile(sourceScanFile, os.O_RDONLY, 0)
		if err != nil {
			log.Fatalf("Error opening source scan file %s: %v", sourceScanFile, err)
		}
		defer sourceScanF.Close()

		targetScanF, err := os.OpenFile(targetScanFile, os.O_RDONLY, 0)
		if err != nil {
			log.Fatalf("Error opening target scan file %s: %v", targetScanFile, err)
		}
		defer targetScanF.Close()

		sourceChangesF, err := os.OpenFile(sourceChangesFile, os.O_RDONLY, 0)
		if err != nil {
			log.Fatalf("Error opening source changes file %s: %v", sourceChangesFile, err)
		}
		defer sourceChangesF.Close()

		dataFiles := &seatbelt.DataFileSet{
			SourceScan:    seatbelt.NewDataFile(sourceScanF),
			TargetScan:    seatbelt.NewDataFile(targetScanF),
			SourceChanges: seatbelt.NewDataFile(sourceChangesF),
			// SourceExtractScan is not typically used in shadow-only update, assumed nil
		}

		// TODO: Implement EXPLAIN ANALYZE logic within UpdateShadow or as a separate function call
		if explainAnalyze {
			fmt.Println("EXPLAIN ANALYZE shadow update requested...")
			plan, err := seatbelt.ExplainAnalyzeUpdateShadow(ctx, seatbeltCfg, dataFiles)
			if err != nil {
				log.Fatalf("Error running EXPLAIN ANALYZE: %v", err)
			}
			fmt.Println("--- EXPLAIN ANALYZE Result ---")
			fmt.Println(plan)
			fmt.Println("-----------------------------")
		}

		fmt.Println("Updating shadow table from files...")
		metrics, err := seatbelt.UpdateShadow(ctx, seatbeltCfg, dataFiles)
		if err != nil {
			log.Fatalf("Error updating shadow table from files: %v", err)
		}
		fmt.Println("Shadow table updated successfully.")

		// Print Validation Metrics
		printMetrics(metrics)

	},
}

var benchmarkCmd = &cobra.Command{
	Use:   "benchmark",
	Short: "Benchmark individual fetch components",
	Long:  `Run and time individual components from the data fetch phase, such as source scan, source extract scan, or target scan.`,
}

var benchSourceScanCmd = &cobra.Command{
	Use:   "source-scan",
	Short: "Benchmark only the source scan operation",
	Long:  `Run and time only the source scan operation, printing timing information and the location of the generated file.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := loadConfig(configFile)
		if err != nil {
			log.Fatalf("Error loading config file '%s': %v", configFile, err)
		}

		ctx := context.Background()

		// Create only the necessary components
		table, err := createTable(cfg)
		if err != nil {
			log.Fatalf("Error creating table component: %v", err)
		}

		source, sourceCleanup, err := createSource(ctx, cfg)
		if err != nil {
			log.Fatalf("Error creating source component: %v", err)
		}
		defer sourceCleanup()

		// Run only source scan
		fmt.Println("Running source scan benchmark...")
		startTime := time.Now()
		sourceScan, err := source.Scan(ctx, table)
		duration := time.Since(startTime)
		if err != nil {
			log.Fatalf("Error during source scan: %v", err)
		}

		// Print results
		fmt.Printf("Source scan completed in %v\n", duration)
		fmt.Printf("Source scan result file: %s\n", sourceScan.Name())
		fmt.Printf("Source scan row count: %d\n", sourceScan.RowCount())
	},
}

var benchSourceExtractScanCmd = &cobra.Command{
	Use:   "source-extract-scan",
	Short: "Benchmark only the source extract scan operation",
	Long:  `Run and time only the source extract scan operation, printing timing information and the location of the generated file.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := loadConfig(configFile)
		if err != nil {
			log.Fatalf("Error loading config file '%s': %v", configFile, err)
		}

		ctx := context.Background()

		// Create only the necessary components
		table, err := createTable(cfg)
		if err != nil {
			log.Fatalf("Error creating table component: %v", err)
		}

		source, sourceCleanup, err := createSource(ctx, cfg)
		if err != nil {
			log.Fatalf("Error creating source component: %v", err)
		}
		defer sourceCleanup()

		// Run only source extract scan
		fmt.Println("Running source extract scan benchmark...")
		startTime := time.Now()
		sourceExtractScan, err := source.ExtractScan(ctx, table)
		duration := time.Since(startTime)
		if err != nil {
			log.Fatalf("Error during source extract scan: %v", err)
		}

		// Print results
		fmt.Printf("Source extract scan completed in %v\n", duration)
		fmt.Printf("Source extract scan result file: %s\n", sourceExtractScan.Name())
		fmt.Printf("Source extract scan row count: %d\n", sourceExtractScan.RowCount())
	},
}

var benchTargetScanCmd = &cobra.Command{
	Use:   "target-scan",
	Short: "Benchmark only the target scan operation",
	Long:  `Run and time only the target scan operation, printing timing information and the location of the generated file.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := loadConfig(configFile)
		if err != nil {
			log.Fatalf("Error loading config file '%s': %v", configFile, err)
		}

		ctx := context.Background()

		// Create only the necessary components
		table, err := createTable(cfg)
		if err != nil {
			log.Fatalf("Error creating table component: %v", err)
		}

		target, targetCleanup, err := createTarget(ctx, cfg)
		if err != nil {
			log.Fatalf("Error creating target component: %v", err)
		}
		defer targetCleanup()

		// Run only target scan
		fmt.Println("Running target scan benchmark...")
		startTime := time.Now()
		targetScan, err := target.Scan(ctx, table)
		duration := time.Since(startTime)
		if err != nil {
			log.Fatalf("Error during target scan: %v", err)
		}

		// Print results
		fmt.Printf("Target scan completed in %v\n", duration)
		fmt.Printf("Target scan result file: %s\n", targetScan.Name())
		fmt.Printf("Target scan row count: %d\n", targetScan.RowCount())
	},
}

func loadConfig(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config AppConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config YAML: %w", err)
	}

	// Apply environment variables from config
	if config.Environment != nil {
		for key, value := range config.Environment {
			currentValue := os.Getenv(key)
			if currentValue == "" {
				fmt.Printf("[CONFIG] Setting environment variable %s=%s\n", key, value)
				os.Setenv(key, value)
			} else {
				fmt.Printf("[CONFIG] Not overriding existing environment variable %s (value: %s)\n", key, currentValue)
			}
		}
	}

	// Basic validation
	if config.SourceConnectionString == "" {
		return nil, fmt.Errorf("source_connection_string is required")
	}
	if config.TargetConnectionString == "" {
		return nil, fmt.Errorf("target_connection_string is required")
	}
	if config.RowMapperName == "" {
		return nil, fmt.Errorf("row_mapper_name is required")
	}
	if config.TableName == "" {
		return nil, fmt.Errorf("table_name is required")
	}
	if len(config.Columns) == 0 {
		return nil, fmt.Errorf("at least one column mapping is required")
	}
	if config.PrimaryKeyName == "" {
		return nil, fmt.Errorf("primary_key_name is required")
	}

	fmt.Println("[CONFIG] config", config)

	return &config, nil
}

// createComponents initializes the Source, Target, Table, and RowMapper based on config
// It now returns the components and cleanup functions for source and target connections.
func createComponents(ctx context.Context, cfg *AppConfig) (seatbelt.Source, seatbelt.Target, seatbelt.Table, func(), func(), error) {
	// Create row mapper and table
	table, err := createTable(cfg)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	// Create source
	source, sourceCleanup, err := createSource(ctx, cfg)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	// Create target
	target, targetCleanup, err := createTarget(ctx, cfg)
	if err != nil {
		sourceCleanup() // Clean up source if target creation fails
		return nil, nil, nil, nil, nil, err
	}

	return source, target, table, sourceCleanup, targetCleanup, nil
}

// createTable creates just the table component based on config
func createTable(cfg *AppConfig) (seatbelt.Table, error) {
	tableDef := seatbelt.TableDefinition{
		TableName:       cfg.TableName,
		TargetTableName: cfg.TargetTableName,
		PrimaryKeyName:  cfg.PrimaryKeyName,
		Columns:         cfg.Columns,
	}

	var rowMapper seatbelt.RowMapperAndHasher
	switch cfg.RowMapperName {
	case "peer_db":
		peerDbMapper := row_mappers.NewPeerDBRowMapper(tableDef)
		rowMapper = seatbelt.NewDefaultRowMapperAndHasher(
			&postgres.PostgresSourceHasher{},
			&clickhouse.ClickHouseTargetHasher{},
			peerDbMapper,
		)
	default:
		return nil, fmt.Errorf("unknown row_mapper_name: %s", cfg.RowMapperName)
	}

	table := &seatbelt.DefaultTable{
		TableDefinition:    tableDef,
		RowMapperAndHasher: rowMapper,
	}

	return table, nil
}

// createSource creates just the source component based on config
func createSource(ctx context.Context, cfg *AppConfig) (seatbelt.Source, func(), error) {
	// --- Create Source (PostgreSQL) ---
	pgPool, err := pgxpool.New(ctx, cfg.SourceConnectionString)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create postgres connection pool: %w", err)
	}
	if err := pgPool.Ping(ctx); err != nil {
		pgPool.Close() // Close pool if ping fails
		return nil, nil, fmt.Errorf("failed to ping postgres database: %w", err)
	}
	source := postgres.NewPostgresSource(pgPool)
	sourceCleanup := func() { pgPool.Close() }
	log.Println("PostgreSQL source connection established.")

	return source, sourceCleanup, nil
}

// createTarget creates just the target component based on config
func createTarget(ctx context.Context, cfg *AppConfig) (seatbelt.Target, func(), error) {
	// --- Create Target (ClickHouse) ---
	chDB, err := sql.Open("clickhouse", cfg.TargetConnectionString)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to open clickhouse connection: %w", err)
	}
	if err := chDB.PingContext(ctx); err != nil {
		chDB.Close()
		return nil, nil, fmt.Errorf("failed to ping clickhouse database: %w", err)
	}
	target := clickhouse.NewClickHouseTarget(chDB)
	targetCleanup := func() { chDB.Close() }
	log.Println("ClickHouse target connection established.")

	return target, targetCleanup, nil
}

// printMetrics formats and prints the validation metrics
func printMetrics(metrics *seatbelt.ValidationMetrics) {
	fmt.Println("--- Validation Metrics ---")
	fmt.Printf("Source Size:     %d\n", metrics.SourceSize)
	fmt.Printf("Target Size:     %d\n", metrics.TargetSize)
	fmt.Printf("Seatbelt Size:   %d\n", metrics.SeatbeltSize)
	fmt.Printf("Valid Rows:      %d\n", metrics.ValidCount)
	fmt.Printf("Pending Rows:    %d\n", metrics.PendingCount)
	fmt.Printf("Error Rows:      %d\n", metrics.ErrorCount)
	fmt.Println("--------------------------")
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "config.yaml", "Path to the configuration file")

	// Flags for 'run' command (also used by 'fetch')
	runCmd.Flags().BoolVar(&fetchDataOnly, "fetch-only", false, "Only fetch data, do not update the shadow table")
	runCmd.Flags().BoolVar(&initialLoad, "initial-load", false, "Perform initial load instead of incremental update")
	rootCmd.AddCommand(runCmd)

	// Flags for 'fetch' command (inherits --config)
	// No extra flags needed as it reuses runCmd logic with fetch-only forced true
	rootCmd.AddCommand(fetchCmd)

	// Flags for 'shadow' command
	shadowCmd.Flags().StringVar(&sourceScanFile, "source-scan", "", "Path to the source scan data file (required)")
	shadowCmd.Flags().StringVar(&targetScanFile, "target-scan", "", "Path to the target scan data file (required)")
	shadowCmd.Flags().StringVar(&sourceChangesFile, "source-changes", "", "Path to the source changes data file (required)")
	shadowCmd.Flags().BoolVar(&explainAnalyze, "explain", false, "Run EXPLAIN ANALYZE on the shadow update query (implementation pending)")
	// Mark flags as required for shadow command
	shadowCmd.MarkFlagRequired("source-scan")
	shadowCmd.MarkFlagRequired("target-scan")
	shadowCmd.MarkFlagRequired("source-changes")
	rootCmd.AddCommand(shadowCmd)

	// Register benchmark commands
	benchmarkCmd.AddCommand(benchSourceScanCmd)
	benchmarkCmd.AddCommand(benchSourceExtractScanCmd)
	benchmarkCmd.AddCommand(benchTargetScanCmd)
	rootCmd.AddCommand(benchmarkCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
