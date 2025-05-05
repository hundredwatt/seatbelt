package postgres

import (
	"fmt"
	"seatbelt/pkg/seatbelt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const NULL_REPRESENTATION = "👻"

func BuildSourceTextExpressionForHashing(tableDef seatbelt.Table) string {
	var concatParts []string
	for _, col := range tableDef.SourceColumns() {
		safeColName := pgx.Identifier{col.Name}.Sanitize()
		concatParts = append(concatParts, fmt.Sprintf("COALESCE(%s::text, '%s')", safeColName, NULL_REPRESENTATION))
	}
	concatenationExpression := strings.Join(concatParts, " || ")
	return concatenationExpression
}
