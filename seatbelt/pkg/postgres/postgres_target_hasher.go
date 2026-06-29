package postgres

import (
	"fmt"
	"seatbelt/pkg/seatbelt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// PostgresTargetHasher hashes destination rows when the target is PostgreSQL (e.g. a MySQL → Postgres
// pipeline). The target hash uses the same hashtextextended port as the source side so that a
// destination row's hash computed in-database matches the hash Seatbelt computes in Go from the
// mapper's canonical string.
type PostgresTargetHasher struct {
	TableDefinition *seatbelt.TableDefinition
}

func (h *PostgresTargetHasher) TargetHash(data string) seatbelt.RowHash {
	// The shadow table stores target signatures as UBIGINT, so reinterpret hashtextextended's signed
	// 64-bit result as unsigned. PostgresTarget.Scan applies the same reinterpretation in SQL.
	return seatbelt.Uint64Hash(uint64(PostgresHashtextextended(data, SEED)))
}

// SQLTextExpressionForTargetHashing builds the canonical concatenation expression over the target
// columns, mirroring PostgresSourceHasher so source and target canonical strings line up.
func (h *PostgresTargetHasher) SQLTextExpressionForTargetHashing() string {
	var concatParts []string
	for _, col := range h.TableDefinition.TargetColumns() {
		safeColName := pgx.Identifier{col.Name}.Sanitize()
		concatParts = append(concatParts, fmt.Sprintf("COALESCE(%s::text, '%s')", safeColName, NULL_REPRESENTATION))
	}
	return strings.Join(concatParts, " || ")
}
