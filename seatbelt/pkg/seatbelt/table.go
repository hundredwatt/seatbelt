package seatbelt

import (
	"encoding/hex"
	"fmt"
)

type ColumnType string

const (
	ColumnTypeInt       ColumnType = "int"
	ColumnTypeFloat     ColumnType = "float"
	ColumnTypeText      ColumnType = "text"
	ColumnTypeNull      ColumnType = ""
	ColumnTypeSmallInt  ColumnType = "smallint"
	ColumnTypeBigInt    ColumnType = "bigint"
	ColumnTypeDouble    ColumnType = "double"
	ColumnTypeBoolean   ColumnType = "boolean"
	ColumnTypeDate      ColumnType = "date"
	ColumnTypeTime      ColumnType = "time"
	ColumnTypeTimestamp ColumnType = "timestamp"
)

type Column struct {
	Name string
	Type ColumnType
}

type ColumnMapping struct {
	Name       string
	SourceType ColumnType
	TargetType ColumnType
}

type TableDefinition struct {
	TableName      string
	PrimaryKeyName string
	Columns        []ColumnMapping
}

func (t *TableDefinition) Name() string {
	return t.TableName
}

func (t *TableDefinition) PrimaryKey() string {
	return t.PrimaryKeyName
}

func (t *TableDefinition) ColumnMapping() []ColumnMapping {
	return t.Columns
}

func (t *TableDefinition) SourceColumns() []Column {
	columns := make([]Column, len(t.Columns))
	for i, column := range t.Columns {
		if column.SourceType == ColumnTypeNull {
			continue
		}
		columns[i] = Column{Name: column.Name, Type: column.SourceType}
	}
	return columns
}

func (t *TableDefinition) TargetColumns() []Column {
	columns := make([]Column, len(t.Columns))
	for i, column := range t.Columns {
		if column.TargetType == ColumnTypeNull {
			continue
		}
		columns[i] = Column{Name: column.Name, Type: column.TargetType}
	}
	return columns
}

/* RowHash Types */
type RowHash interface {
	// Empty interface to allow either uint64 or [16]byte
	// Implementations should return either uint64 or [16]byte
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

/* Table Interface */
type Table interface {
	Name() string
	PrimaryKey() string // TODO: Compound primary key support
	ColumnMapping() []ColumnMapping
	SourceColumns() []Column
	TargetColumns() []Column

	RowMapperAndHasher
}

type DefaultTable struct {
	TableDefinition
	RowMapperAndHasher
}
