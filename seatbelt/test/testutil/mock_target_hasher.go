package testutil

import (
	"crypto/md5"
	"fmt"

	"seatbelt/pkg/seatbelt"
)

// For testing purposes only
type MockTargetHasher struct {
}

func (h *MockTargetHasher) TransformSourceToCommon(row []interface{}) (string, error) {
	return fmt.Sprintf("%v", row), nil
}

func (h *MockTargetHasher) TransformTargetToCommon(row []interface{}) (string, error) {
	return fmt.Sprintf("%v", row), nil
}

func (h *MockTargetHasher) TargetHash(data string) seatbelt.RowHash {
	return seatbelt.Hex16Hash(md5.Sum([]byte(data)))
}
