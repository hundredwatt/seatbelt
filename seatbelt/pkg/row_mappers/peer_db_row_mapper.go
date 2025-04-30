package row_mappers

import "fmt"

type PeerDBRowMapper struct {
}

func (m *PeerDBRowMapper) TransformSourceToCommon(row []interface{}) (string, error) {
	return fmt.Sprintf("%s%s%s%s", transformNil(row[0]), transformNil(row[1]), transformFloats(row[2]), transformFloats(row[3])), nil
}

func (m *PeerDBRowMapper) TransformTargetToCommon(row []interface{}) (string, error) {
	return fmt.Sprintf("%d,%d,%f,%f", row[0], row[1], row[2], row[3]), nil
}

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
	return string_value
}

func transformNil(value interface{}) interface{} {
	if value == nil {
		return "0"
	}
	return value
}
