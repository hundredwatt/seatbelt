"""Transformations for the Seatbelt Demo simulator."""

from typing import Any, Optional
from datetime import datetime, date
from .column_types import ColumnType

class Transformations:
    """Class responsible for data transformations between source and target"""
    
    @staticmethod
    def transform_source_to_target(source_value: Any, column_type: ColumnType, 
                                  target_type: Optional[ColumnType] = None) -> Any:
        """Transform a value from source type to target type if needed"""
        if source_value is None or not target_type or column_type == target_type:
            # For DECIMAL type, ensure we always format with 2 decimal places
            if column_type == ColumnType.DECIMAL and source_value is not None:
                return round(float(source_value), 2)
            # For FLOAT type with implicit float32 in target
            if column_type == ColumnType.FLOAT and source_value is not None and target_type == ColumnType.FLOAT32:
                return f"{float(source_value):.7g}"
            # For INTEGER type with implicit integer32 in target
            if column_type == ColumnType.INTEGER and source_value is not None and target_type == ColumnType.INTEGER32:
                # Check if value is within int32 bounds
                if -2147483648 <= source_value <= 2147483647:
                    return source_value
                else:
                    # Return NULL for out-of-bounds values
                    return None
            return source_value
            
        # Transform based on target type
        if target_type == ColumnType.INTEGER:
            # Convert to integer
            try:
                return int(source_value)
            except (ValueError, TypeError):
                return 0
        elif target_type == ColumnType.INTEGER32:
            # Convert to int32 with bounds checking
            try:
                int_value = int(source_value)
                # Check if value is within int32 bounds
                if -2147483648 <= int_value <= 2147483647:
                    return int_value
                else:
                    # Return NULL for out-of-bounds values
                    return None
            except (ValueError, TypeError):
                return None
        elif target_type == ColumnType.FLOAT:
            # Convert to float
            try:
                return float(source_value)
            except (ValueError, TypeError):
                return 0.0
        elif target_type == ColumnType.FLOAT32:
            # Convert to float32 (using string representation with 7 significant digits)
            try:
                return f"{float(source_value):.7g}"
            except (ValueError, TypeError):
                return "0.0"
        elif target_type == ColumnType.DECIMAL:
            # Convert to decimal (formatted as float with fixed precision)
            try:
                return round(float(source_value), 2)
            except (ValueError, TypeError):
                return 0.00
        elif target_type == ColumnType.STRING:
            # Convert to string
            return str(source_value)
        elif target_type == ColumnType.BOOLEAN:
            # Convert to boolean
            return bool(source_value)
        elif target_type == ColumnType.DATE:
            # Convert to date
            if isinstance(source_value, datetime):
                return source_value.date()
            return source_value
        elif target_type == ColumnType.DATETIME:
            # Convert to datetime
            if isinstance(source_value, date) and not isinstance(source_value, datetime):
                return datetime.combine(source_value, datetime.min.time())
            return source_value
        else:
            return source_value
    
    @staticmethod
    def format_target_for_validation(target_value: Any, target_type: Optional[ColumnType] = None) -> Any:
        """Format target value for validation to ensure consistent signatures"""
        if target_value is None:
            return None
            
        if target_type == ColumnType.FLOAT32:
            # Ensure consistent float32 formatting if it's not already a string
            if not isinstance(target_value, str):
                return f"{float(target_value):.7g}"
        elif target_type == ColumnType.DECIMAL:
            # For decimal type, ensure it's a float for consistent signatures
            if isinstance(target_value, int):
                return float(target_value)
        elif target_type == ColumnType.INTEGER32:
            # Ensure int32 bounds are respected in validation
            if isinstance(target_value, int) and not (-2147483648 <= target_value <= 2147483647):
                return None
                
        return target_value 