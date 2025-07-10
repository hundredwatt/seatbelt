package row_mappers

import (
	"encoding/json"
	"fmt"
	"seatbelt/pkg/seatbelt"
	"seatbelt/pkg/typesystem" // Import typesystem
	"sort"
	"strings"
	"time"
)

type PeerDBRowMapper struct {
	tableDef seatbelt.TableDefinition
}

// NewPeerDBRowMapper creates a new PeerDBRowMapper with the given table definition and database names.
func NewPeerDBRowMapper(tableDef seatbelt.TableDefinition) *PeerDBRowMapper {
	return &PeerDBRowMapper{
		tableDef: tableDef,
	}
}

// TransformSourceToCommon generates a common string representation from a source row.
// It iterates through the columns defined in the table definition.
func (m *PeerDBRowMapper) TransformSourceToCommon(row []interface{}) (string, error) {
	if len(row) != len(m.tableDef.SourceColumns()) {
		return "", fmt.Errorf("row length (%d) does not match number of source columns (%d)", len(row), len(m.tableDef.SourceColumns()))
	}

	var commonParts []string
	for i, col := range m.tableDef.SourceColumns() { // Iterate based on config order
		// Pass source database name and type string
		transformedValue := transformValue(row[i], col.TypeInfo.Family)
		commonParts = append(commonParts, transformedValue)
	}
	return strings.Join(commonParts, ""), nil // Consider a delimiter if needed
}

// TransformTargetToCommon generates a common string representation from a target row.
// It iterates through the columns defined in the table definition.
func (m *PeerDBRowMapper) TransformTargetToCommon(row []interface{}) (string, error) {
	if len(row) != len(m.tableDef.TargetColumns()) {
		return "", fmt.Errorf("row length (%d) does not match number of target columns (%d)", len(row), len(m.tableDef.TargetColumns()))
	}

	var commonParts []string
	sourceColumns := m.tableDef.SourceColumns()
	targetColumns := m.tableDef.TargetColumns()

	for i, col := range targetColumns {
		if row[i] == nil {
			commonParts = append(commonParts, "0")
			continue
		}

		// For JSON normalization, check the source column type family
		// since JSON columns are stored as strings in the target (ClickHouse)
		var sourceFamily typesystem.TypeFamily = typesystem.UnknownFamily
		if i < len(sourceColumns) && sourceColumns[i].TypeInfo != nil {
			sourceFamily = sourceColumns[i].TypeInfo.Family
		}

		// Use source type family for JSON detection, target type family for other transformations
		if sourceFamily == typesystem.JSONFamily {
			// JSON/JSONB columns from PostgreSQL are delivered to ClickHouse as strings
			// We need to normalize them the same way as the source
			jsonStr := fmt.Sprintf("%v", row[i])
			commonParts = append(commonParts, normalizeJSON(jsonStr))
		} else {
			switch col.TypeInfo.Family {
			case typesystem.FloatFamily, typesystem.DecimalFamily:
				commonParts = append(commonParts, fmt.Sprintf("%f", row[i]))
			case typesystem.IntegerFamily:
				commonParts = append(commonParts, fmt.Sprintf("%d", row[i]))
			default:
				commonParts = append(commonParts, fmt.Sprintf("%v", row[i]))
			}
		}
	}
	return strings.Join(commonParts, ""), nil // Consider a delimiter if needed
}

// transformValue handles nil and type-specific transformations based on TypeFamily.
func transformValue(value interface{}, family typesystem.TypeFamily) string {
	if value == nil {
		return "0" // Use NULL representation for nil (Changed from "0")
	}

	switch family {
	case typesystem.FloatFamily:
		return transformFloatString(value.(string))
	case typesystem.DecimalFamily:
		return transformDecimalString(value.(string))
	case typesystem.DateTimeFamily:
		return transformDateTimeString(value.(string))
	case typesystem.JSONFamily:
		return normalizeJSON(value.(string))
	default:
		return value.(string)
	}
}

// formatValueByFamily formats a value based on its general TypeFamily.
func formatValueByFamily(value interface{}, family typesystem.TypeFamily) string {
	if value == nil {
		return "0"
	}

	switch family {
	case typesystem.IntegerFamily:
		// Use %d for all integer types detected by reflection or known family
		return fmt.Sprintf("%d", value)
	case typesystem.FloatFamily, typesystem.DecimalFamily:
		// Use %f for standard float/decimal formatting
		// Consider more specific formatting based on DatabaseTypeInfo if needed later
		return fmt.Sprintf("%f", value)
	case typesystem.BooleanFamily:
		return fmt.Sprintf("%t", value)
	case typesystem.DateTimeFamily, typesystem.DateFamily:
		// Attempt to assert to time.Time
		if t, ok := value.(time.Time); ok {
			return t.UTC().Format(time.RFC3339Nano)
		} else {
			// Fallback if it's not a time.Time (e.g., a string already)
			return fmt.Sprintf("%v", value)
		}
	case typesystem.JSONFamily:
		// JSON family needs special normalization for deterministic key ordering
		jsonStr := fmt.Sprintf("%s", value)
		return normalizeJSON(jsonStr)
	case typesystem.StringFamily, typesystem.UUIDFamily, typesystem.EnumFamily, typesystem.NetworkFamily, typesystem.GeometricFamily, typesystem.XMLFamily:
		// Treat these families generally as strings
		return fmt.Sprintf("%s", value)
	case typesystem.BinaryFamily:
		// How should binary be represented? Hex? Base64?
		// For now, using %v, which might not be ideal for hashing.
		// Consider hex encoding: fmt.Sprintf("%x", value)
		return fmt.Sprintf("%v", value)
	// Add cases for ArrayFamily, MapFamily, TupleFamily, VariantFamily etc. if needed
	default: // Includes UnknownFamily
		// Fallback to general string conversion
		return fmt.Sprintf("%v", value)
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

func transformDecimalString(s string) string {
	// Remove trailing zeros after decimal point
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		// If we removed all digits after decimal point, remove the decimal point too
		s = strings.TrimRight(s, ".")
	}
	return s
}

func transformDateTimeString(s string) string {
	// Parse the timestamp string
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err == nil {
		// Format with microsecond precision
		return t.Format("2006-01-02 15:04:05.000000")
	}
	return s
}

// normalizeJSON normalizes a JSON string by parsing it and re-encoding with sorted keys
// This ensures deterministic string representation for JSON/JSONB columns
func normalizeJSON(jsonStr string) string {
	if jsonStr == "" {
		return ""
	}

	// Parse the JSON string into a generic interface{}
	var parsed interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		// If parsing fails, return the original string
		return jsonStr
	}

	// Sort the keys recursively
	normalized := sortJSONKeys(parsed)

	// Re-encode with deterministic key ordering
	result, err := json.Marshal(normalized)
	if err != nil {
		// If marshaling fails, return the original string
		return jsonStr
	}

	// Add spaces after commas and colons for consistent formatting
	return string(result)
}

// sortJSONKeys recursively sorts keys in JSON objects to ensure deterministic ordering
func sortJSONKeys(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		// Create a new map with sorted keys
		sorted := make(map[string]interface{})

		// Get all keys and sort them
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		// Add items to new map in sorted key order, recursively sorting values
		for _, key := range keys {
			sorted[key] = sortJSONKeys(v[key])
		}
		return sorted

	case []interface{}:
		// For arrays, recursively sort each element
		for i, item := range v {
			v[i] = sortJSONKeys(item)
		}
		return v

	default:
		// For primitive values (string, number, bool, null), return as-is
		return v
	}
}

// addSpacesAfterSeparators inserts a space after each comma and colon that are not inside a string.
// This ensures deterministic formatting of the JSON string while preserving string contents.
func addSpacesAfterSeparators(s string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/10) // small optimization: pre-allocate slightly more capacity

	inString := false
	escape := false

	for _, r := range s {
		if r == '"' && !escape {
			inString = !inString
		}

		if !inString && (r == ',' || r == ':') {
			b.WriteRune(r)
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}

		// Track escape character state when inside strings
		if r == '\\' && !escape {
			escape = true
		} else {
			escape = false
		}
	}

	return b.String()
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
