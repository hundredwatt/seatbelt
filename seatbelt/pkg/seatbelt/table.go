package seatbelt

import (
	"seatbelt/pkg/typesystem"
)

type DatabaseName string

const (
	POSTGRES DatabaseName = "postgres"
	CLICKHOUSE DatabaseName = "clickhouse"
)

func (d DatabaseName) String() string {
	return string(d)
}

type Column struct {
	Name       string
	TypeInfo   *typesystem.DatabaseTypeInfo
}

type ColumnMapping struct {
	Name       string
	SourceType string `yaml:"source_type"` // Changed from ColumnType
	TargetType string `yaml:"target_type"` // Changed from ColumnType
}

type TableDefinition struct {
	SourceDatabase	DatabaseName
	TargetDatabase	DatabaseName
	TableName          string
	TargetTableName    string
	PrimaryKeyName     string
	Columns            []ColumnMapping
}

func (t *TableDefinition) Name() string {
	return t.TableName
}

func (t *TableDefinition) TargetName() string {
	if t.TargetTableName == "" {
		return t.TableName
	}
	return t.TargetTableName
}

func (t *TableDefinition) PrimaryKey() string {
	return t.PrimaryKeyName
}

func (t *TableDefinition) ColumnMapping() []ColumnMapping {
	return t.Columns
}

func (t *TableDefinition) SourceColumns() []Column {
	columns := make([]Column, 0, len(t.Columns)) // Initialize with capacity
	for _, column := range t.Columns {
		// Check if SourceType is empty instead of ColumnTypeNull
		if column.SourceType == "" {
			continue
		}
		columns = append(columns, Column{
			Name:       column.Name,
			TypeInfo:   typesystem.TypeRegistry.GetTypeInfo(t.SourceDatabase.String(), column.SourceType),
		})
	}
	return columns
}

func (t *TableDefinition) TargetColumns() []Column {
	columns := make([]Column, 0, len(t.Columns)) // Initialize with capacity
	for _, column := range t.Columns {
		// Check if TargetType is empty instead of ColumnTypeNull
		if column.TargetType == "" {
			continue
		}
		columns = append(columns, Column{
			Name:       column.Name,
			TypeInfo:   typesystem.TypeRegistry.GetTypeInfo(t.TargetDatabase.String(), column.TargetType),
		})
	}
	return columns
}

/* Table Interface */
type Table interface {
	Name() string
	TargetName() string
	PrimaryKey() string // TODO: Compound primary key support
	SourceColumns() []Column
	TargetColumns() []Column

	RowMapperAndHasher
}

type DefaultTable struct {
	TableDefinition
	RowMapperAndHasher
}
