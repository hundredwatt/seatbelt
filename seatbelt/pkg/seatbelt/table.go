package seatbelt

// Assuming typesystem might be needed later, add import

type Column struct {
	Name string
	Type string // Changed from ColumnType
}

type ColumnMapping struct {
	Name       string
	SourceType string `yaml:"source_type"` // Changed from ColumnType
	TargetType string `yaml:"target_type"` // Changed from ColumnType
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
	columns := make([]Column, 0, len(t.Columns)) // Initialize with capacity
	for _, column := range t.Columns {
		// Check if SourceType is empty instead of ColumnTypeNull
		if column.SourceType == "" {
			continue
		}
		columns = append(columns, Column{Name: column.Name, Type: column.SourceType})
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
		columns = append(columns, Column{Name: column.Name, Type: column.TargetType})
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
