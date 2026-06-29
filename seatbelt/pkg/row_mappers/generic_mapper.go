package row_mappers

import (
	"fmt"

	"seatbelt/pkg/seatbelt"
)

// nullRepresentation must match the ghost sentinel used by the SQL-side canonical expressions
// (postgres.NULL_REPRESENTATION) so in-database and in-Go hashes line up.
const nullRepresentation = "👻"

// GenericRowMapper is an identity mapper for pipelines that don't transform values in ways that change
// their text representation (e.g. integer and string columns copied 1:1 by a tool like Sling). It
// concatenates each column's default string form, substituting a ghost sentinel for NULLs — matching
// the `COALESCE(col::text, '👻') || ...` expression the SQL hashers build on both sides.
//
// It is deliberately simple. Pipelines with JSON, floating-point, decimal, or timestamp columns whose
// representations differ between source and destination need a purpose-built mapper — see
// wasm-mappers/ for the pluggable WASM approach.
type GenericRowMapper struct{}

func NewGenericRowMapper() *GenericRowMapper {
	return &GenericRowMapper{}
}

func canonicalize(row []interface{}) (string, error) {
	out := ""
	for _, value := range row {
		if value == nil {
			out += nullRepresentation
		} else {
			out += fmt.Sprintf("%v", value)
		}
	}
	return out, nil
}

func (m *GenericRowMapper) TransformSourceToCommon(row []interface{}) (string, error) {
	return canonicalize(row)
}

func (m *GenericRowMapper) TransformTargetToCommon(row []interface{}) (string, error) {
	return canonicalize(row)
}

// Ensure GenericRowMapper satisfies the interface.
var _ seatbelt.RowMapper = (*GenericRowMapper)(nil)
