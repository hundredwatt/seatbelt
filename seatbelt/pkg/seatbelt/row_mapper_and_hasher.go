package seatbelt

import (
	"encoding/hex"
	"fmt"
)

type RowHash interface {
	String() string
}

type RowHashPair struct {
	SourceHash RowHash
	TargetHash RowHash
}

type Uint64Hash uint64

func (h Uint64Hash) String() string {
	return fmt.Sprintf("%d", h)
}

type Int64Hash int64

func (h Int64Hash) String() string {
	return fmt.Sprintf("%d", h)
}

type Hex32Hash [32]byte

func (h Hex32Hash) String() string {
	return hex.EncodeToString(h[:])
}

type Hex16Hash [16]byte

func (h Hex16Hash) String() string {
	return hex.EncodeToString(h[:])
}

type SourceHasher interface {
	FormatSource(row []interface{}) (string, error)
	SourceHash(data string) RowHash
}

type TargetHasher interface {
	TransformSourceToCommon(row []interface{}) (string, error)
	TransformTargetToCommon(row []interface{}) (string, error)
	TargetHash(data string) RowHash
}

type RowMapperAndHasher interface {
	SourceHasher
	TargetHasher
}

type DefaultRowMapperAndHasher struct {
	SourceHasher
	TargetHasher
}

func NewDefaultRowMapperAndHasher(sourceHasher SourceHasher, targetHasher TargetHasher) *DefaultRowMapperAndHasher {
	return &DefaultRowMapperAndHasher{
		SourceHasher: sourceHasher,
		TargetHasher: targetHasher,
	}
}
