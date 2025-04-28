package clickhouse

import (
	"fmt"
	"log"
	"strings"

	"seatbelt/pkg/targets"
)

// ClickHouseTargetHasher implements the TargetHasher interface for ClickHouse.
// It handles the transformation and hashing of data according to ClickHouse's
// specific requirements.
type ClickHouseTargetHasher struct {
	// Columns to include in the hash calculation
	hashColumns []string
	// Enable debug logging
	debug bool
}

// NewClickHouseTargetHasher creates a new ClickHouse target hasher.
func NewClickHouseTargetHasher(hashColumns []string, debug bool) *ClickHouseTargetHasher {
	return &ClickHouseTargetHasher{
		hashColumns: hashColumns,
		debug:       debug,
	}
}

// Transform prepares the row data into a string suitable for ClickHouse hashing.
// It concatenates the values of the specified columns.
func (h *ClickHouseTargetHasher) Transform(row map[string]interface{}) (string, error) {
	var builder strings.Builder

	for _, colName := range h.hashColumns {
		val, ok := row[colName]
		if !ok {
			return "", fmt.Errorf("missing hash column '%s' in row data", colName)
		}

		if val == nil {
			// Use zero for NULL values
			builder.WriteString("0")
		} else if bytesVal, typeOk := val.([]byte); typeOk {
			// Append text representation of the value
			builder.WriteString(string(bytesVal))
		} else {
			// Handle unexpected types
			if h.debug {
				log.Printf("Warning: Unexpected type for hash column '%s': %T. Treating as empty string.", colName, val)
			}
			builder.WriteString("")
		}
	}

	result := builder.String()
	if h.debug {
		log.Printf("DEBUG: Transformed row data to: '%s'", result)
	}

	return result, nil
}

// Hash computes the hash of the transformed data string using ClickHouse's XXH3 algorithm.
// The seed parameter is ignored as ClickHouse's XXH3 implementation doesn't use a seed.
func (h *ClickHouseTargetHasher) Hash(data string, seed int64) uint64 {
	// Convert string to bytes for hashing
	dataBytes := []byte(data)
	
	// Compute hash using XXH3
	hash := XXH3(dataBytes)
	
	if h.debug {
		log.Printf("DEBUG: Hashed '%s' to %d", data, hash)
	}
	
	return hash
}

// Ensure ClickHouseTargetHasher implements the TargetHasher interface
var _ targets.TargetHasher = (*ClickHouseTargetHasher)(nil)
