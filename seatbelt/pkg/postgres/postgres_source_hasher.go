package postgres

import (
	"fmt"
	"seatbelt/pkg/seatbelt"
)

const SEED = 1337

type PostgresSourceHasher struct {
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
