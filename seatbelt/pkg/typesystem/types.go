package typesystem

// TypeFamily represents a general category of data types.
type TypeFamily string

const (
	UnknownFamily   TypeFamily = "unknown"
	IntegerFamily   TypeFamily = "integer"
	FloatFamily     TypeFamily = "float"
	DecimalFamily   TypeFamily = "decimal"
	StringFamily    TypeFamily = "string"
	BooleanFamily   TypeFamily = "boolean"
	DateFamily      TypeFamily = "date"
	DateTimeFamily  TypeFamily = "datetime"
	BinaryFamily    TypeFamily = "binary"
	UUIDFamily      TypeFamily = "uuid"
	NetworkFamily   TypeFamily = "network"
	GeometricFamily TypeFamily = "geometric"
	JSONFamily      TypeFamily = "json"
	XMLFamily       TypeFamily = "xml"
	ArrayFamily     TypeFamily = "array"
	EnumFamily      TypeFamily = "enum"
	MapFamily       TypeFamily = "map"
	TupleFamily     TypeFamily = "tuple"
	VariantFamily   TypeFamily = "variant"
	// Add other families as needed (Range, TextSearch, etc.)
)

// TypeAttribute represents characteristics like precision, scale, or length.
type TypeAttribute struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// We might add regex/parsing hints here later if needed
}

// DatabaseTypeInfo holds structured information about a specific database type.
type DatabaseTypeInfo struct {
	Name          string          `yaml:"name"` // The primary name (e.g., "integer", "character varying")
	Family        TypeFamily      `yaml:"family"`
	Aliases       []string        `yaml:"aliases"`       // e.g., ["int", "int4"] for "integer"
	Attributes    []TypeAttribute `yaml:"attributes"`    // e.g., precision/scale for numeric, length for varchar
	Documentation string          `yaml:"documentation"` // Link or brief description
}

// DatabaseTypesConfig is the top-level structure for a database's type definition YAML file.
type DatabaseTypesConfig struct {
	DatabaseName string             `yaml:"database_name"`
	Types        []DatabaseTypeInfo `yaml:"types"`
}
