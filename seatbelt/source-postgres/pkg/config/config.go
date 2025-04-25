package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration
type Config struct {
	Database struct {
		StdConnString  string `yaml:"std_conn_string"`
		ReplConnString string `yaml:"repl_conn_string"`
	} `yaml:"database"`

	Replication struct {
		SlotName        string `yaml:"slot_name"`
		PublicationName string `yaml:"publication_name"`
		IdleTimeoutStr  string `yaml:"idle_timeout"`
	} `yaml:"replication"`

	Table struct {
		Name        string   `yaml:"name"`
		IDColumn    string   `yaml:"id_column"`
		HashColumns []string `yaml:"hash_columns"`
	} `yaml:"table"`

	HashSeed int64 `yaml:"hash_seed"`

	Output struct {
		SelectCSVPath      string `yaml:"select_csv_path"`
		ReplicationCSVPath string `yaml:"replication_csv_path"`
	} `yaml:"output"`

	Debug bool `yaml:"debug"`

	// Parsed fields
	IdleTimeout time.Duration `yaml:"-"` // Parsed from IdleTimeoutStr
	SchemaName  string        `yaml:"-"`
	TableName   string        `yaml:"-"`
}

// LoadConfig reads the configuration from the specified YAML file
func LoadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file '%s': %w", filePath, err)
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file '%s': %w", filePath, err)
	}

	// Parse IdleTimeout
	if cfg.Replication.IdleTimeoutStr != "" {
		duration, err := time.ParseDuration(cfg.Replication.IdleTimeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid replication.idle_timeout format '%s': %w", cfg.Replication.IdleTimeoutStr, err)
		}
		if duration <= 0 {
			return nil, fmt.Errorf("replication.idle_timeout must be positive, got '%s'", cfg.Replication.IdleTimeoutStr)
		}
		cfg.IdleTimeout = duration
	} else {
		// Default if not set
		cfg.IdleTimeout = 10 * time.Second
	}

	// Parse schema and table name
	parts := strings.SplitN(cfg.Table.Name, ".", 2)
	if len(parts) == 2 {
		cfg.SchemaName = parts[0]
		cfg.TableName = parts[1]
	} else if len(parts) == 1 {
		cfg.SchemaName = "public" // Default schema
		cfg.TableName = parts[0]
	} else {
		return nil, fmt.Errorf("invalid table.name format: '%s'", cfg.Table.Name)
	}

	// Basic validation
	if cfg.Database.StdConnString == "" || cfg.Database.ReplConnString == "" {
		return nil, fmt.Errorf("database connection strings cannot be empty")
	}
	if cfg.Replication.SlotName == "" || cfg.Replication.PublicationName == "" {
		return nil, fmt.Errorf("replication slot and publication names cannot be empty")
	}
	if cfg.TableName == "" || cfg.Table.IDColumn == "" {
		return nil, fmt.Errorf("table name and id_column cannot be empty")
	}
	if len(cfg.Table.HashColumns) == 0 {
		return nil, fmt.Errorf("table hash_columns must contain at least one column")
	}
	if cfg.Output.SelectCSVPath == "" || cfg.Output.ReplicationCSVPath == "" {
		return nil, fmt.Errorf("output CSV paths cannot be empty")
	}

	return &cfg, nil
}
