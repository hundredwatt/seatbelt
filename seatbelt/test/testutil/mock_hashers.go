package testutil

import (
	"crypto/md5"
	"fmt"

	"seatbelt/pkg/seatbelt"

	"golang.org/x/crypto/blake2b"
)

type MockSourceHasher struct {
}

func (h *MockSourceHasher) FormatSource(row []interface{}) (string, error) {
	return fmt.Sprintf("%v", row), nil
}

func (h *MockSourceHasher) SourceHash(data string) seatbelt.RowHash {
	return seatbelt.Hex32Hash(blake2b.Sum256([]byte(data)))
}

func (h *MockSourceHasher) SQLTextExpressionForSourceHashing() string {
	return "'error - not implemented'"
}

type MockTargetHasher struct {
}

func (h *MockTargetHasher) TargetHash(data string) seatbelt.RowHash {
	return seatbelt.Hex16Hash(md5.Sum([]byte(data)))
}

func (h *MockTargetHasher) SQLTextExpressionForTargetHashing() string {
	return "'error - not implemented'"
}

type MockRowMapper struct {
}

func (h *MockRowMapper) TransformSourceToCommon(row []interface{}) (string, error) {
	return fmt.Sprintf("%v", row), nil
}

func (h *MockRowMapper) TransformTargetToCommon(row []interface{}) (string, error) {
	return fmt.Sprintf("%v", row), nil
}