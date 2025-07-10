package main

import (
	"context"
	"database/sql" // Import standard SQL package for ClickHouse
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	"github.com/spf13/pflag"
)

// Set during build time using -ldflags
var version = "dev"

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
	primaryKeys       []int64
	columnNames       []string

	// Flags specific to shadow command
	shadowPath              string
	shadowInitialLoad       bool // Use a separate variable for shadow's initial-load flag
	shadowSourceExtractFile string
)

var rootCmd = &cobra.Command{
	Use:   "seatbelt",
	Short: "Seatbelt is a tool for data validation between sources and targets",
	Long:  `Seatbelt helps ensure data consistency by comparing data between a source and a target system using cryptographic hashes and maintaining a shadow table.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// You can add global setup here if needed
		slog.SetLogLoggerLevel(slog.LevelInfo)
		fmt.Fprintf(os.Stderr, "Seatbelt %s\n", version)
	},
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the full Seatbelt process (fetch data and update shadow)",
	Long:  `Loads configuration, fetches data from source and target, updates the shadow table, and prints validation metrics.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := loadConfig(configFile)
		if err != nil {
			slog.Error("Error loading config file", "error", err)
			os.Exit(1)
		}

		ctx := context.Background()

		// 1. Create Components (Source, Target, Table, RowMapper)
		source, target, table, sourceCleanup, targetCleanup, err := createComponents(ctx, cfg)
		if err != nil {
			slog.Error("Error creating components", "error", err)
			os.Exit(1)
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
		slog.Info("Fetching data...")
		dataFiles, err := seatbelt.FetchData(ctx, seatbeltCfg)
		if err != nil {
			slog.Error("Error fetching data", "error", err)
			os.Exit(1)
		}
		slog.Info("Data fetched successfully.")
		if !initialLoad {
			fmt.Fprintf(os.Stderr, "Source scan completed in %s, %s rows (%s rows/s, %.2f MB/s)\n", dataFiles.SourceScan.GenerationTime, humanize(dataFiles.SourceScan.RowCount()), humanize(int64(float64(dataFiles.SourceScan.RowCount())/dataFiles.SourceScan.GenerationTime.Seconds())), float64(dataFiles.SourceScan.SourceDataSize)/dataFiles.SourceScan.GenerationTime.Seconds()/1024/1024)
		}
		fmt.Fprintf(os.Stderr, "Target scan completed in %s, %s rows (%s rows/s, %.2f MB/s)\n", dataFiles.TargetScan.GenerationTime, humanize(dataFiles.TargetScan.RowCount()), humanize(int64(float64(dataFiles.TargetScan.RowCount())/dataFiles.TargetScan.GenerationTime.Seconds())), float64(dataFiles.TargetScan.SourceDataSize)/dataFiles.TargetScan.GenerationTime.Seconds()/1024/1024)
		if !initialLoad && dataFiles.SourceChanges != nil {
			slog.Debug("  Source Changes", "file", dataFiles.SourceChanges.Name(), "rows", dataFiles.SourceChanges.RowCount())
		}
		if dataFiles.SourceExtractScan != nil {
			fmt.Fprintf(os.Stderr, "Source extract scan completed in %s, %d rows (%.2f rows/s, %.2f MB/s)\n", dataFiles.SourceExtractScan.GenerationTime, dataFiles.SourceExtractScan.RowCount(), float64(dataFiles.SourceExtractScan.RowCount())/dataFiles.SourceExtractScan.GenerationTime.Seconds(), float64(dataFiles.SourceExtractScan.SourceDataSize)/dataFiles.SourceExtractScan.GenerationTime.Seconds()/1024/1024)
		}

		if fetchDataOnly {
			slog.Debug("Fetch data only mode enabled. Skipping shadow update.")
			return // Exit early as requested
		}

		// 3. Update Shadow
		slog.Debug("Updating shadow table...")
		metrics, err := seatbelt.UpdateShadow(ctx, seatbeltCfg, dataFiles)
		if err != nil {
			slog.Error("Error updating shadow table", "error", err)
			os.Exit(1)
		}
		slog.Debug("Shadow table updated successfully.")

		// 4. Print Validation Metrics
		printMetrics(metrics)

		if metrics.ErrorCount > 0 {
			errorPks := strings.Split(metrics.ErrorPKs, ";")
			slog.Info("PKs with errors", "pks", errorPks)

			// Write error PKs JSON to temp file
			tempDir := os.Getenv(seatbelt.EnvTempDir)
			file, err := os.CreateTemp(tempDir, fmt.Sprintf("seatbelt-errors-%s-*.json", cfg.TableName))
			if err != nil {
				slog.Error("Failed to create temp file for error PKs", "error", err)
				os.Exit(1)
			}
			defer file.Close()

			if _, err := file.WriteString(metrics.ErrorPKsJSON); err != nil {
				slog.Error("Failed to write error PKs to temp file", "error", err)
				os.Exit(1)
			}
			slog.Info("Error PKs written to temp file", "file", file.Name())
		}
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
		// Validate required flags conditionally
		if targetScanFile == "" {
			slog.Error("--target-scan file path must be provided")
			os.Exit(1)
		}
		if shadowInitialLoad {
			if shadowSourceExtractFile == "" {
				slog.Error("--source-extract-scan file path must be provided when --initial-load is true")
				os.Exit(1)
			}
		} else {
			if sourceScanFile == "" || sourceChangesFile == "" {
				slog.Error("--source-scan and --source-changes file paths must be provided when --initial-load is false")
				os.Exit(1)
			}
		}

		// Minimal config needed for UpdateShadow when files are provided
		seatbeltCfg := &seatbelt.Config{
			ShadowPath:  shadowPath,        // Use shadow path from flag
			InitialLoad: shadowInitialLoad, // Use initial load from flag
			// Table, Source, Target are not strictly needed for shadow update
		}

		ctx := context.Background()

		// --- Open required files ---
		targetScanF, err := os.OpenFile(targetScanFile, os.O_RDONLY, 0)
		if err != nil {
			slog.Error("Error opening target scan file", "error", err, "file", targetScanFile)
			os.Exit(1)
		}
		defer targetScanF.Close()

		var sourceScanF, sourceChangesF, sourceExtractF *os.File
		dataFiles := &seatbelt.DataFileSet{
			TargetScan: seatbelt.NewDataFile(targetScanF),
		}

		if shadowInitialLoad {
			sourceExtractF, err = os.OpenFile(shadowSourceExtractFile, os.O_RDONLY, 0)
			if err != nil {
				slog.Error("Error opening source extract scan file", "error", err, "file", shadowSourceExtractFile)
				os.Exit(1)
			}
			defer sourceExtractF.Close()
			dataFiles.SourceExtractScan = seatbelt.NewDataFile(sourceExtractF)
		} else {
			sourceScanF, err = os.OpenFile(sourceScanFile, os.O_RDONLY, 0)
			if err != nil {
				slog.Error("Error opening source scan file", "error", err, "file", sourceScanFile)
				os.Exit(1)
			}
			defer sourceScanF.Close()
			dataFiles.SourceScan = seatbelt.NewDataFile(sourceScanF)

			sourceChangesF, err = os.OpenFile(sourceChangesFile, os.O_RDONLY, 0)
			if err != nil {
				slog.Error("Error opening source changes file", "error", err, "file", sourceChangesFile)
				os.Exit(1)
			}
			defer sourceChangesF.Close()
			dataFiles.SourceChanges = seatbelt.NewDataFile(sourceChangesF)
		}
		// --- End File Opening ---

		// Handle EXPLAIN ANALYZE
		if explainAnalyze {
			slog.Debug("EXPLAIN ANALYZE shadow update requested...")
			// Pass seatbeltCfg directly, which now contains InitialLoad
			plan, err := seatbelt.ExplainAnalyzeUpdateShadow(ctx, seatbeltCfg, dataFiles)
			if err != nil {
				slog.Error("Error running EXPLAIN ANALYZE", "error", err)
				os.Exit(1)
			}
			slog.Debug("--- EXPLAIN ANALYZE Result ---")
			slog.Debug(plan)
			slog.Debug("-----------------------------")
			return
		}

		// Run UpdateShadow
		slog.Debug("Updating shadow table from files...")
		// Pass seatbeltCfg directly, which now contains InitialLoad
		metrics, err := seatbelt.UpdateShadow(ctx, seatbeltCfg, dataFiles)
		if err != nil {
			slog.Error("Error updating shadow table from files", "error", err)
			os.Exit(1)
		}
		slog.Debug("Shadow table updated successfully.")

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
			slog.Error("Error loading config file", "error", err)
			os.Exit(1)
		}

		ctx := context.Background()

		// Create TableDefinition
		tableDef, err := createTable(cfg)
		if err != nil {
			slog.Error("Error creating table definition", "error", err)
			os.Exit(1)
		}

		// Create Source
		source, sourceCleanup, err := createSource(ctx, cfg)
		if err != nil {
			slog.Error("Error creating source component", "error", err)
			os.Exit(1)
		}
		defer sourceCleanup()

		// Create RowMapper
		if cfg.RowMapperName != "peer_db" {
			slog.Error("Benchmark currently only supports 'peer_db' mapper")
			os.Exit(1)
		}
		peerDbMapper := row_mappers.NewPeerDBRowMapper(tableDef)
		rowMapper := seatbelt.NewDefaultRowMapperAndHasher(
			&postgres.PostgresSourceHasher{TableDefinition: &tableDef},
			&clickhouse.ClickHouseTargetHasher{TableDefinition: &tableDef},
			peerDbMapper,
		)

		// Create full Table instance
		table := &seatbelt.DefaultTable{
			TableDefinition:    tableDef,
			RowMapperAndHasher: rowMapper,
		}

		// Run only source scan
		slog.Debug("Running source scan benchmark...")
		startTime := time.Now()
		sourceScan, err := source.Scan(ctx, table)
		duration := time.Since(startTime)
		if err != nil {
			slog.Error("Error during source scan", "error", err)
			os.Exit(1)
		}

		// Print results
		slog.Debug("Source scan completed", "duration", duration, "file", sourceScan.Name(), "rows", sourceScan.RowCount())
	},
}

var benchSourceExtractScanCmd = &cobra.Command{
	Use:   "source-extract-scan",
	Short: "Benchmark only the source extract scan operation",
	Long:  `Run and time only the source extract scan operation, printing timing information and the location of the generated file.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := loadConfig(configFile)
		if err != nil {
			slog.Error("Error loading config file", "error", err)
			os.Exit(1)
		}

		ctx := context.Background()

		// Create TableDefinition
		tableDef, err := createTable(cfg)
		if err != nil {
			slog.Error("Error creating table definition", "error", err)
			os.Exit(1)
		}

		// Create Source
		source, sourceCleanup, err := createSource(ctx, cfg)
		if err != nil {
			slog.Error("Error creating source component", "error", err)
			os.Exit(1)
		}
		defer sourceCleanup()

		// Create RowMapper
		if cfg.RowMapperName != "peer_db" {
			slog.Error("Benchmark currently only supports 'peer_db' mapper")
			os.Exit(1)
		}
		peerDbMapper := row_mappers.NewPeerDBRowMapper(tableDef)
		rowMapper := seatbelt.NewDefaultRowMapperAndHasher(
			&postgres.PostgresSourceHasher{TableDefinition: &tableDef},
			&clickhouse.ClickHouseTargetHasher{TableDefinition: &tableDef},
			peerDbMapper,
		)

		// Create full Table instance
		table := &seatbelt.DefaultTable{
			TableDefinition:    tableDef,
			RowMapperAndHasher: rowMapper,
		}

		// Run only source extract scan
		slog.Debug("Running source extract scan benchmark...")
		startTime := time.Now()
		sourceExtractScan, err := source.ExtractScan(ctx, table)
		duration := time.Since(startTime)
		if err != nil {
			slog.Error("Error during source extract scan", "error", err)
			os.Exit(1)
		}

		// Print results
		slog.Debug("Source extract scan completed", "duration", duration, "file", sourceExtractScan.Name(), "rows", sourceExtractScan.RowCount())
	},
}

var benchTargetScanCmd = &cobra.Command{
	Use:   "target-scan",
	Short: "Benchmark only the target scan operation",
	Long:  `Run and time only the target scan operation, printing timing information and the location of the generated file.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := loadConfig(configFile)
		if err != nil {
			slog.Error("Error loading config file", "error", err)
			os.Exit(1)
		}

		ctx := context.Background()

		// Create TableDefinition
		tableDef, err := createTable(cfg)
		if err != nil {
			slog.Error("Error creating table definition", "error", err)
			os.Exit(1)
		}

		// Create Target
		target, targetCleanup, err := createTarget(ctx, cfg)
		if err != nil {
			slog.Error("Error creating target component", "error", err)
			os.Exit(1)
		}
		defer targetCleanup()

		// Create RowMapper
		if cfg.RowMapperName != "peer_db" {
			slog.Error("Benchmark currently only supports 'peer_db' mapper")
			os.Exit(1)
		}
		peerDbMapper := row_mappers.NewPeerDBRowMapper(tableDef)
		rowMapper := seatbelt.NewDefaultRowMapperAndHasher(
			&postgres.PostgresSourceHasher{TableDefinition: &tableDef},
			&clickhouse.ClickHouseTargetHasher{TableDefinition: &tableDef},
			peerDbMapper,
		)

		// Create full Table instance
		table := &seatbelt.DefaultTable{
			TableDefinition:    tableDef,
			RowMapperAndHasher: rowMapper,
		}

		// Run only target scan
		slog.Debug("Running target scan benchmark...")
		startTime := time.Now()
		targetScan, err := target.Scan(ctx, table)
		duration := time.Since(startTime)
		if err != nil {
			slog.Error("Error during target scan", "error", err)
			os.Exit(1)
		}

		// Print results
		slog.Debug("Target scan completed", "duration", duration, "file", targetScan.Name(), "rows", targetScan.RowCount())
	},
}

var inspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Inspect specific rows by primary keys",
	Long:  `Inspect specific rows from source and target databases by their primary keys. Runs InspectScan and InspectExtractScan on the source and InspectScan on the target.`,
	Run: func(cmd *cobra.Command, args []string) {
		slog.SetLogLoggerLevel(slog.LevelDebug)

		cfg, err := loadConfig(configFile)
		if err != nil {
			slog.Error("Error loading config file", "error", err)
			os.Exit(1)
		}

		if len(primaryKeys) == 0 {
			slog.Error("At least one primary key must be provided")
			os.Exit(1)
		}

		ctx := context.Background()

		// Create Components (Source, Target, Table)
		source, target, table, sourceCleanup, targetCleanup, err := createComponents(ctx, cfg)
		if err != nil {
			slog.Error("Error creating components", "error", err)
			os.Exit(1)
		}
		// Defer cleanup functions to close connections when done
		defer sourceCleanup()
		defer targetCleanup()

		// Filter table columns if column names are provided
		if len(columnNames) > 0 {
			// Create a map for quick lookup of column names
			columnsMap := make(map[string]bool)
			for _, col := range columnNames {
				columnsMap[col] = true
			}

			// Always ensure the primary key is included
			columnsMap[cfg.PrimaryKeyName] = true

			// Get the current table definition
			defaultTable, ok := table.(*seatbelt.DefaultTable)
			if !ok {
				slog.Error("Unexpected table type, cannot filter columns")
				os.Exit(1)
			}

			// Filter the table definition's columns based on the provided list
			var filteredColumns []seatbelt.ColumnMapping
			for _, col := range defaultTable.Columns {
				if columnsMap[col.Name] {
					filteredColumns = append(filteredColumns, col)
				}
			}

			// Create a new table definition with filtered columns
			filteredTableDef := seatbelt.TableDefinition{
				SourceDatabase:  defaultTable.SourceDatabase,
				TargetDatabase:  defaultTable.TargetDatabase,
				TableName:       defaultTable.TableName,
				TargetTableName: defaultTable.TargetTableName,
				PrimaryKeyName:  defaultTable.PrimaryKeyName,
				Columns:         filteredColumns,
			}

			// Create a new row mapper with the filtered table definition
			var rowMapper seatbelt.RowMapperAndHasher
			switch cfg.RowMapperName {
			case "peer_db":
				peerDbMapper := row_mappers.NewPeerDBRowMapper(filteredTableDef)
				rowMapper = seatbelt.NewDefaultRowMapperAndHasher(
					&postgres.PostgresSourceHasher{TableDefinition: &filteredTableDef},
					&clickhouse.ClickHouseTargetHasher{TableDefinition: &filteredTableDef},
					peerDbMapper,
				)
			default:
				slog.Error("Unknown row_mapper_name", "name", cfg.RowMapperName)
				os.Exit(1)
			}

			// Create a new table with the filtered definition
			table = &seatbelt.DefaultTable{
				TableDefinition:    filteredTableDef,
				RowMapperAndHasher: rowMapper,
			}

			slog.Debug("Using filtered columns", "columns", columnNames)
		}

		// Cast source and target to their inspector interfaces
		sourceInspector, ok := source.(seatbelt.SourceInspector)
		if !ok {
			slog.Error("Source does not implement SourceInspector interface")
			os.Exit(1)
		}

		targetInspector, ok := target.(seatbelt.TargetInspector)
		if !ok {
			slog.Error("Target does not implement TargetInspector interface")
			os.Exit(1)
		}

		// Run all inspect methods
		slog.Debug("Running source inspect scan...")
		sourceScan, err := sourceInspector.InspectScan(ctx, table, primaryKeys)
		if err != nil {
			slog.Error("Error running source inspect scan", "error", err)
			os.Exit(1)
		}
		slog.Debug("Source inspect scan completed", "file", sourceScan.Name(), "rows", sourceScan.RowCount())

		slog.Debug("Running source inspect extract scan...")
		sourceExtractScan, err := sourceInspector.InspectExtractScan(ctx, table, primaryKeys)
		if err != nil {
			slog.Error("Error running source inspect extract scan", "error", err)
			os.Exit(1)
		}
		slog.Debug("Source inspect extract scan completed", "file", sourceExtractScan.Name(), "rows", sourceExtractScan.RowCount())

		slog.Debug("Running target inspect scan...")
		targetScan, err := targetInspector.InspectScan(ctx, table, primaryKeys)
		if err != nil {
			slog.Error("Error running target inspect scan", "error", err)
			os.Exit(1)
		}
		slog.Debug("Target inspect scan completed", "file", targetScan.Name(), "rows", targetScan.RowCount())

		slog.Debug("\nInspect Results:")
		slog.Debug("----------------------------")
		slog.Debug("Source inspect scan file", "file", sourceScan.Name())
		slog.Debug("Source inspect extract scan file", "file", sourceExtractScan.Name())
		slog.Debug("Target inspect scan file", "file", targetScan.Name())
		slog.Debug("----------------------------")
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
				slog.Debug("Setting environment variable", "key", key, "value", value)
				os.Setenv(key, value)
			} else {
				slog.Debug("Not overriding existing environment variable", "key", key, "current_value", currentValue)
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

	slog.Debug("Loaded config", "config", config)

	return &config, nil
}

// createComponents initializes the Source, Target, Table, and RowMapper based on config
// It now returns the components and cleanup functions for source and target connections.
func createComponents(ctx context.Context, cfg *AppConfig) (seatbelt.Source, seatbelt.Target, seatbelt.Table, func(), func(), error) {

	// --- Create Table Definition --- (Moved from createTable)
	tableDef := seatbelt.TableDefinition{
		SourceDatabase:  seatbelt.POSTGRES,
		TargetDatabase:  seatbelt.CLICKHOUSE,
		TableName:       cfg.TableName,
		TargetTableName: cfg.TargetTableName,
		PrimaryKeyName:  cfg.PrimaryKeyName,
		Columns:         cfg.Columns,
	}

	// --- Create Source --- (Determine source DB name)
	// TODO: Infer from cfg.SourceConnectionString scheme
	source, sourceCleanup, err := createSource(ctx, cfg)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	// --- Create Target --- (Determine target DB name)
	// TODO: Infer from cfg.TargetConnectionString scheme
	target, targetCleanup, err := createTarget(ctx, cfg)
	if err != nil {
		sourceCleanup() // Clean up source if target creation fails
		return nil, nil, nil, nil, nil, err
	}

	// --- Create Row Mapper and final Table --- (Moved from createTable)
	var rowMapper seatbelt.RowMapperAndHasher
	switch cfg.RowMapperName {
	case "peer_db":
		// Pass the determined database names
		peerDbMapper := row_mappers.NewPeerDBRowMapper(tableDef)
		rowMapper = seatbelt.NewDefaultRowMapperAndHasher(
			&postgres.PostgresSourceHasher{TableDefinition: &tableDef},
			&clickhouse.ClickHouseTargetHasher{TableDefinition: &tableDef},
			peerDbMapper,
		)
	default:
		sourceCleanup()
		targetCleanup()
		return nil, nil, nil, nil, nil, fmt.Errorf("unknown row_mapper_name: %s", cfg.RowMapperName)
	}

	table := &seatbelt.DefaultTable{
		TableDefinition:    tableDef,
		RowMapperAndHasher: rowMapper,
	}

	return source, target, table, sourceCleanup, targetCleanup, nil
}

// createTable now only returns the definition structure.
// RowMapper/Table creation happens in createComponents.
func createTable(cfg *AppConfig) (seatbelt.TableDefinition, error) {
	tableDef := seatbelt.TableDefinition{
		SourceDatabase:  seatbelt.POSTGRES,
		TargetDatabase:  seatbelt.CLICKHOUSE,
		TableName:       cfg.TableName,
		TargetTableName: cfg.TargetTableName,
		PrimaryKeyName:  cfg.PrimaryKeyName,
		Columns:         cfg.Columns,
	}
	// Basic validation (can add more specific TableDefinition validation if needed)
	if tableDef.TableName == "" || tableDef.PrimaryKeyName == "" || len(tableDef.Columns) == 0 {
		return seatbelt.TableDefinition{}, fmt.Errorf("invalid table definition in config: missing table name, primary key, or columns")
	}
	return tableDef, nil
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
	slog.Debug("PostgreSQL source connection established.")

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
	slog.Debug("ClickHouse target connection established.")

	return target, targetCleanup, nil
}

func humanize(n int64) string {
	if n == 0 {
		return "0"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}

	s := fmt.Sprintf("%d", n)
	le := len(s)
	if le <= 3 {
		return sign + s
	}

	res := make([]byte, le+((le-1)/3))
	i := le - 1
	j := len(res) - 1
	for i >= 0 {
		res[j] = s[i]
		if i > 0 && (le-i)%3 == 0 {
			j--
			res[j] = ','
		}
		i--
		j--
	}
	return sign + string(res)
}

// printMetrics formats and prints the validation metrics
func printMetrics(metrics *seatbelt.ValidationMetrics) {
	fmt.Println("--- Validation Metrics ---")
	fmt.Printf("%-18s %25s\n", "Metric", "Count")
	fmt.Println("---------------------------------------------")
	fmt.Printf("%-18s %25s\n", "Source Row Count", humanize(metrics.SourceSize))
	fmt.Printf("%-18s %25s\n", "Target Row Count", humanize(metrics.TargetSize))
	fmt.Printf("%-18s %25s\n", "Valid Rows", humanize(metrics.ValidCount))
	fmt.Printf("%-18s %25s\n", "Pending Rows", humanize(metrics.PendingCount))
	fmt.Printf("%-18s %25s\n", "Error Rows", humanize(metrics.ErrorCount))
	fmt.Println("---------------------------------------------")
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "config.yaml", "Path to the configuration file")
	rootCmd.PersistentFlags().StringVarP(&configFile, "table", "t", "config.yaml", "Path to the configuration file (alias for --config)")

	// Flags for 'run' command (also used by 'fetch')
	runCmd.Flags().BoolVar(&fetchDataOnly, "fetch-only", false, "Only fetch data, do not update the shadow table")
	runCmd.Flags().BoolVar(&initialLoad, "initial-load", false, "Perform initial load instead of incremental update")
	rootCmd.AddCommand(runCmd)

	// Flags for 'fetch' command (inherits --config)
	// No extra flags needed as it reuses runCmd logic with fetch-only forced true
	rootCmd.AddCommand(fetchCmd)

	// Flags for 'shadow' command
	shadowCmd.Flags().StringVar(&sourceScanFile, "source-scan", "", "Path to the source scan data file (required if not initial load)")
	shadowCmd.Flags().StringVar(&targetScanFile, "target-scan", "", "Path to the target scan data file (required)")
	shadowCmd.Flags().StringVar(&sourceChangesFile, "source-changes", "", "Path to the source changes data file (required if not initial load)")
	shadowCmd.Flags().StringVar(&shadowSourceExtractFile, "source-extract-scan", "", "Path to the source extract scan data file (required for initial load)")
	shadowCmd.Flags().StringVar(&shadowPath, "shadow-path", ":memory:", "Path to the shadow database file or :memory:")
	shadowCmd.Flags().BoolVar(&shadowInitialLoad, "initial-load", false, "Perform initial load using source-extract-scan")
	shadowCmd.Flags().BoolVar(&explainAnalyze, "explain", false, "Run EXPLAIN ANALYZE on the shadow update query")
	// Remove explicit required marking - validation happens in Run
	// shadowCmd.MarkFlagRequired("source-scan") // Removed
	// shadowCmd.MarkFlagRequired("target-scan") // Removed - checked in Run
	// shadowCmd.MarkFlagRequired("source-changes") // Removed
	rootCmd.AddCommand(shadowCmd)

	// Register benchmark commands
	benchmarkCmd.AddCommand(benchSourceScanCmd)
	benchmarkCmd.AddCommand(benchSourceExtractScanCmd)
	benchmarkCmd.AddCommand(benchTargetScanCmd)
	rootCmd.AddCommand(benchmarkCmd)

	// Register inspect command
	inspectCmd.Flags().Int64SliceVar(&primaryKeys, "pks", []int64{}, "Primary keys to inspect (comma-separated)")
	inspectCmd.Flags().StringSliceVar(&columnNames, "cols", []string{}, "Column names to include (comma-separated)")
	// Set column-names as an alias for cols
	inspectCmd.Flags().SetNormalizeFunc(func(f *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "column-names" {
			name = "cols"
		}
		return pflag.NormalizedName(name)
	})
	inspectCmd.MarkFlagRequired("pks")
	rootCmd.AddCommand(inspectCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		slog.Error("Error executing root command", "error", err)
		os.Exit(1)
	}
}
