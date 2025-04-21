"""Column type definitions for the Seatbelt Demo simulator."""

from enum import Enum

class ColumnType(Enum):
    """Supported data column types"""
    INTEGER = "integer"
    INTEGER32 = "integer32"
    FLOAT = "float"
    FLOAT32 = "float32"
    DECIMAL = "decimal"
    STRING = "string"
    BOOLEAN = "boolean"
    DATE = "date"
    DATETIME = "datetime"
    JSON = "json" 