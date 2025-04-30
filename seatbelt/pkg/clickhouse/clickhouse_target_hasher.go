package clickhouse

import (
	"seatbelt/pkg/seatbelt"

	"github.com/zeebo/xxh3"
)

type ClickHouseTargetHasher struct {
}

func (h *ClickHouseTargetHasher) TargetHash(data string) seatbelt.RowHash {
	return seatbelt.Uint64Hash(xxh3.Hash([]byte(data)))
}
