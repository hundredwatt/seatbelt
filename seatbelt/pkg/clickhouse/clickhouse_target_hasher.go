package clickhouse

import (
	"fmt"
	"seatbelt/pkg/seatbelt"
	"strings"

	"github.com/zeebo/xxh3"
)

const NULL_REPRESENTATION = "👻"

type ClickHouseTargetHasher struct {
	TableDefinition *seatbelt.TableDefinition
}

func (h *ClickHouseTargetHasher) TargetHash(data string) seatbelt.RowHash {
	return seatbelt.Uint64Hash(xxh3.Hash([]byte(data)))
}

func (h *ClickHouseTargetHasher) SQLTextExpressionForTargetHashing() string {
	var concatParts []string
	for _, col := range h.TableDefinition.TargetColumns() {
		concatParts = append(concatParts, fmt.Sprintf("ifNull(CAST(%s AS String), '%s')", col.Name, NULL_REPRESENTATION))
	}
	return fmt.Sprintf("concat(%s)", strings.Join(concatParts, ", "))
}
