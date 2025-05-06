package typesystem

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed configs/*.yaml
var typeConfigs embed.FS

// TypeRegistry provides access to database type information.
var TypeRegistry = newRegistry()

// registry holds the parsed type information.
type registry struct {
	mu sync.RWMutex
	// Map key is lowercase "<database>_<type_name_or_alias>"
	// e.g., "postgres_integer", "postgres_int", "clickhouse_int32"
	types map[string]*DatabaseTypeInfo
}

func newRegistry() *registry {
	r := &registry{
		types: make(map[string]*DatabaseTypeInfo),
	}
	err := r.loadTypes()
	if err != nil {
		// This happens at init time, so panic is acceptable.
		panic(fmt.Sprintf("Failed to load type definitions: %v", err))
	}
	return r
}

func (r *registry) loadTypes() error {
	configDir := "configs"
	files, err := typeConfigs.ReadDir(configDir)
	if err != nil {
		return fmt.Errorf("failed to read embedded directory %s: %w", configDir, err)
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}

		filePath := filepath.Join(configDir, file.Name())
		data, err := typeConfigs.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", filePath, err)
		}

		var config DatabaseTypesConfig
		if err := yaml.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to unmarshal YAML file %s: %w", filePath, err)
		}

		if config.DatabaseName == "" {
			return fmt.Errorf("database_name missing in %s", filePath)
		}

		dbPrefix := strings.ToLower(config.DatabaseName) + "_"

		for i := range config.Types {
			typeInfo := &config.Types[i] // Use pointer to the element in the slice

			if typeInfo.Name == "" {
				return fmt.Errorf("type name missing in %s for database %s", filePath, config.DatabaseName)
			}

			// Register primary name
			key := dbPrefix + strings.ToLower(typeInfo.Name)
			if _, exists := r.types[key]; exists {
				return fmt.Errorf("duplicate type definition for %s in %s", typeInfo.Name, filePath)
			}
			r.types[key] = typeInfo

			// Register aliases
			for _, alias := range typeInfo.Aliases {
				aliasKey := dbPrefix + strings.ToLower(alias)
				if _, exists := r.types[aliasKey]; exists {
					// It's possible an alias conflicts with another primary name or alias, check if it points to the *same* typeInfo
					if r.types[aliasKey] != typeInfo {
						return fmt.Errorf("duplicate alias '%s' (for %s) conflicts with existing type %s in %s", alias, typeInfo.Name, r.types[aliasKey].Name, filePath)
					}
					// If it's the same typeInfo, it's redundant but okay.
				} else {
					r.types[aliasKey] = typeInfo
				}
			}
		}
	}
	return nil
}

// GetTypeInfo retrieves the type information for a given database and type name.
// It handles parameterized types (e.g., decimal(10,2), varchar(100))
// by extracting the base type name before lookup.
// Type names are case-insensitive. Returns nil if the type is not found.
func (r *registry) GetTypeInfo(databaseName, typeName string) *DatabaseTypeInfo {
	normalizedTypeName := strings.ToLower(strings.TrimSpace(typeName))
	baseTypeName := normalizedTypeName

	// Check for parameters (e.g., varchar(100), decimal(10, 2))
	if parenIndex := strings.Index(normalizedTypeName, "("); parenIndex != -1 {
		baseTypeName = strings.TrimSpace(normalizedTypeName[:parenIndex])
		// We are not parsing the actual parameters inside () for now,
		// just extracting the base type name for lookup.
	}

	key := strings.ToLower(databaseName) + "_" + baseTypeName

	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.types[key]
	if !ok {
		// If not found, maybe it's an alias? (This part already handles aliases)
		// The original map key construction handles aliases correctly during load.
		// Re-check with the original non-parsed normalized name just in case
		// it was something like 'int4' which doesn't have params but is an alias.
		if baseTypeName != normalizedTypeName {
			key = strings.ToLower(databaseName) + "_" + normalizedTypeName
			info, ok = r.types[key]
			if !ok {
				panic(fmt.Sprintf("type not found: %s.%s", databaseName, typeName))
			}
		} else {
			panic(fmt.Sprintf("type not found: %s.%s", databaseName, typeName))
		}
	}
	return info
}

// GetTypeFamily retrieves the family of a given database type.
// Returns UnknownFamily if the type is not found.
// Uses GetTypeInfo internally, so it also handles parameterized types.
func (r *registry) GetTypeFamily(databaseName, typeName string) TypeFamily {
	info := r.GetTypeInfo(databaseName, typeName)
	if info == nil {
		return UnknownFamily
	}
	return info.Family
}
