package mysql

import (
	"fmt"
	"strings"

	"seatbelt/pkg/postgres"
	"seatbelt/pkg/seatbelt"
)

// nullRepresentation must match the ghost sentinel used by the Postgres-side canonical expressions so
// hashes line up across source and destination.
const nullRepresentation = "👻"

// MySQLSourceHasher computes source-row hashes for a MySQL source. It reuses the Go port of
// PostgreSQL's hashtextextended so the hash domain matches a PostgreSQL destination in a
// MySQL → Postgres pipeline.
type MySQLSourceHasher struct {
	TableDefinition *seatbelt.TableDefinition
}

func (h *MySQLSourceHasher) FormatSource(row []interface{}) (string, error) {
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

func (h *MySQLSourceHasher) SourceHash(data string) seatbelt.RowHash {
	return seatbelt.Int64Hash(postgres.PostgresHashtextextended(data, postgres.SEED))
}

// SQLTextExpressionForSourceHashing returns a MySQL canonical-string expression. The current MySQL
// source hashes in Go (see MySQLSource.Scan), so this is provided for completeness/parity.
func (h *MySQLSourceHasher) SQLTextExpressionForSourceHashing() string {
	var parts []string
	for _, col := range h.TableDefinition.Columns {
		if col.SourceType == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("COALESCE(CAST(`%s` AS CHAR), '%s')", col.Name, nullRepresentation))
	}
	return "CONCAT(" + strings.Join(parts, ", ") + ")"
}
