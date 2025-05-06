package row_mappers

import (
	"fmt"
	"seatbelt/pkg/seatbelt"
	"seatbelt/pkg/typesystem" // Import typesystem
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
	for i, col := range m.tableDef.TargetColumns() {
		switch col.TypeInfo.Family {
		case typesystem.FloatFamily, typesystem.DecimalFamily:
			commonParts = append(commonParts, fmt.Sprintf("%f", row[i]))
		case typesystem.IntegerFamily:
			commonParts = append(commonParts, fmt.Sprintf("%d", row[i]))
		default:
			commonParts = append(commonParts, fmt.Sprintf("%v", row[i]))
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
	case typesystem.StringFamily, typesystem.UUIDFamily, typesystem.EnumFamily, typesystem.NetworkFamily, typesystem.GeometricFamily, typesystem.JSONFamily, typesystem.XMLFamily:
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
		// Convert from local time to UTC, accounting for DST
		// The time is parsed without timezone info, so we need to set the location first
		loc, _ := time.LoadLocation("America/Denver") // Mountain Time
		localTime := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
		utcTime := localTime.UTC()
		// Format with microsecond precision
		return utcTime.Format("2006-01-02 15:04:05.000000")
	}
	return s
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
