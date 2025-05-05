package postgres

import (
	"fmt"
	"seatbelt/pkg/seatbelt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const SEED = 1337
const NULL_REPRESENTATION = "👻"

type PostgresSourceHasher struct {
	TableDefinition *seatbelt.TableDefinition
}

func (h *PostgresSourceHasher) FormatSource(row []interface{}) (string, error) {
	// fmt.Printf("[0] value: %v, type: %T\n", row[0], row[0])
	rowString := ""
	for _, value := range row {
		if value == nil {
			rowString += "👻"
		} else {
			rowString += fmt.Sprintf("%v", value)
		}
	}
	return rowString, nil
}

func (h *PostgresSourceHasher) SourceHash(data string) seatbelt.RowHash {
	return seatbelt.Int64Hash(PostgresHashtextextended(data, SEED))
}

func (h *PostgresSourceHasher) SQLTextExpressionForSourceHashing() string {
	var concatParts []string
	for _, col := range h.TableDefinition.SourceColumns() {
		safeColName := pgx.Identifier{col.Name}.Sanitize()
		concatParts = append(concatParts, fmt.Sprintf("COALESCE(%s::text, '%s')", safeColName, NULL_REPRESENTATION))
	}
	concatenationExpression := strings.Join(concatParts, " || ")
	return concatenationExpression
}
