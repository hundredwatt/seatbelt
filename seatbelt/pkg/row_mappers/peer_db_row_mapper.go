package row_mappers

import (
	"fmt"
	"seatbelt/pkg/seatbelt"
	"strings"
	"time"
)

type PeerDBRowMapper struct {
	tableDef seatbelt.TableDefinition // Store the table definition
}

// NewPeerDBRowMapper creates a new PeerDBRowMapper with the given table definition.
func NewPeerDBRowMapper(tableDef seatbelt.TableDefinition) *PeerDBRowMapper {
	return &PeerDBRowMapper{tableDef: tableDef}
}

// TransformSourceToCommon generates a common string representation from a source row.
// It iterates through the columns defined in the table definition.
func (m *PeerDBRowMapper) TransformSourceToCommon(row []interface{}) (string, error) {
	if len(row) != len(m.tableDef.SourceColumns()) {
		return "", fmt.Errorf("row length (%d) does not match number of columns (%d)", len(row), len(m.tableDef.Columns))
	}

	var commonParts []string
	for i, col := range m.tableDef.SourceColumns() { // Iterate based on config order
		// Use SourceType for source transformation
		transformedValue := transformValue(row[i], col.Type)
		commonParts = append(commonParts, transformedValue)
	}
	return strings.Join(commonParts, ""), nil // Use a consistent delimiter
}

// TransformTargetToCommon generates a common string representation from a target row.
// It iterates through the columns defined in the table definition.
func (m *PeerDBRowMapper) TransformTargetToCommon(row []interface{}) (string, error) {
	if len(row) != len(m.tableDef.TargetColumns()) {
		return "", fmt.Errorf("row length (%d) does not match number of columns (%d)", len(row), len(m.tableDef.TargetColumns()))
	}

	var commonParts []string
	for i, col := range m.tableDef.TargetColumns() {
		switch col.Type {
		case seatbelt.ColumnTypeInt:
			commonParts = append(commonParts, fmt.Sprintf("%d", row[i]))
		case seatbelt.ColumnTypeFloat:
			commonParts = append(commonParts, fmt.Sprintf("%f", row[i]))
		default:
			commonParts = append(commonParts, fmt.Sprintf("%s", row[i]))
		}
	}
	return strings.Join(commonParts, ""), nil // Use a consistent delimiter
}

// transformValue handles nil and type-specific transformations.
func transformValue(value interface{}, dataType seatbelt.ColumnType) string {
	if value == nil {
		return "0" // Use NULL representation for nil
	}

	// Convert based on the actual type and specified data type
	switch v := value.(type) {
	case string:
		// Apply float-specific string transformations only if the column type indicates float/double
		if dataType == seatbelt.ColumnTypeFloat || dataType == seatbelt.ColumnTypeDouble {
			return transformFloatString(v)
		}
		return v // Return other strings as-is
	case float64:
		// TODO: Standardize float formatting if necessary
		return fmt.Sprintf("%f", v)
	case float32:
		return fmt.Sprintf("%f", v)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case bool:
		return fmt.Sprintf("%t", v)
	case time.Time:
		// Example: Format timestamp consistently. Adjust format as needed.
		return v.UTC().Format(time.RFC3339Nano)
	default:
		// Fallback to general string conversion, might need more specific handlers
		return fmt.Sprintf("%v", v)
	}
}

// transformFloatString handles special float string values and scientific notation.
func transformFloatString(s string) string {
	switch s {
	case "Infinity":
		return "inf"
	case "-Infinity":
		return "-inf"
	case "NaN":
		return "nan"
	default:
		// Normalize scientific notation like "e+" to "e"
		return strings.ReplaceAll(s, "e+", "e")
	}
}

/*
// Original functions kept for reference
func transformFloats(value interface{}) string {
	if value == nil {
		return transformNil(value).(string)
	}
	string_value := value.(string)
	if string_value == "Infinity" {
		return "inf"
	}
	if string_value == "-Infinity" {
		return "-inf"
	}
	if string_value == "NaN" {
		return "nan"
	}

	string_value = strings.ReplaceAll(string_value, "e+", "e")

	return string_value
}

func transformNil(value interface{}) interface{} {
	if value == nil {
		return "0"
	}
	return value
}
*/
