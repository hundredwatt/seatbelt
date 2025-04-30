package clickhouse

import (
	"fmt"

	"seatbelt/pkg/seatbelt"

	"github.com/zeebo/xxh3"
)

type ClickHouseTargetHasher struct {
}

func (h *ClickHouseTargetHasher) TransformSourceToCommon(row []interface{}) (string, error) {
	return fmt.Sprintf("%v", row), nil
}

func (h *ClickHouseTargetHasher) TransformTargetToCommon(row []interface{}) (string, error) {
	return fmt.Sprintf("%v", row), nil
}

func (h *ClickHouseTargetHasher) TargetHash(data string) seatbelt.RowHash {
	return seatbelt.Uint64Hash(xxh3.Hash([]byte(data)))
}
