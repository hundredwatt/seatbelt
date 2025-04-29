package postgres2

import (
	"fmt"
	"seatbelt/pkg/seatbelt"
)

const SEED = 1337

type PostgresDefaultSourceHasher struct {
}

func (h *PostgresDefaultSourceHasher) FormatSource(row []interface{}) (string, error) {
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

func (h *PostgresDefaultSourceHasher) SourceHash(data string) seatbelt.RowHash {
	return PostgresHashtextextended(data, SEED)
}
