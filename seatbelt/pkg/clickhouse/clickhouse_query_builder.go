package clickhouse

import (
	"fmt"
	"seatbelt/pkg/seatbelt"
	"strings"
)

const NULL_REPRESENTATION = "👻"

func BuildTargetTextExpressionForHashing(tableDef seatbelt.Table) string {
	var concatParts []string
	for _, col := range tableDef.TargetColumns() {
		concatParts = append(concatParts, fmt.Sprintf("ifNull(CAST(%s AS String), '%s')", col.Name, NULL_REPRESENTATION))
	}
	return fmt.Sprintf("concat(%s)", strings.Join(concatParts, ", "))
}
