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