package seatbelt

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
	SourceType ColumnType `yaml:"source_type"`
	TargetType ColumnType `yaml:"target_type"`
}

type TableDefinition struct {
	TableName       string
	TargetTableName string
	PrimaryKeyName  string
	Columns         []ColumnMapping
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

/* Table Interface */
type Table interface {
	Name() string
	TargetName() string
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
