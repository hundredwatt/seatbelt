package clickhouse

import (
	"github.com/zeebo/xxh3"
)

// XXH3 computes the 64-bit XXH3 hash using a seed of 0, matching ClickHouse's default behavior.
func XXH3(data []byte) uint64 {
	return xxh3.Hash(data)
}
