"""Utilities for schema handling and conversion in the Seatbelt Demo application."""

import logging
from typing import Dict, Any, Optional

from .column_types import ColumnType
from .database import SchemaDefinition, ColumnDefinition, InitialData

def convert_schema_dict(schema_dict: Dict[str, Any]) -> SchemaDefinition:
    """Convert a schema dictionary from YAML/JSON to a SchemaDefinition object
    
    Args:
        schema_dict: Dictionary containing schema definition with 'columns' key
        
    Returns:
        SchemaDefinition object populated with columns from the dictionary
    """
    schema = SchemaDefinition()
    
    # Add columns from the dictionary
    for column_dict in schema_dict.get('columns', []):
        column_type_str = column_dict.get('type', 'string')
        try:
            column_type = ColumnType(column_type_str)
        except ValueError:
            logging.warning(f"Invalid column type '{column_type_str}' for column '{column_dict.get('name')}', using STRING")
            column_type = ColumnType.STRING
        
        # Handle target_type conversion to enum
        target_type = None
        if 'target_type' in column_dict:
            target_type_str = column_dict.get('target_type')
            try:
                target_type = ColumnType(target_type_str)
            except ValueError:
                logging.warning(f"Invalid target_type '{target_type_str}' for column '{column_dict.get('name')}', using None")
        
        computed_from = column_dict.get('computed_from')
            
        # Create the column with all properties from dictionary
        column = ColumnDefinition(
            name=column_dict.get('name'),
            type=column_type,
            nullable=column_dict.get('nullable', False),
            target_type=target_type,
            sync_to_target=column_dict.get('sync_to_target', True),
            target_only=column_dict.get('target_only', False),
            computed_from=computed_from
        )
        schema.add_column(column)
        
    return schema

def convert_initial_data_dict(initial_data_dict: Dict[str, Any]) -> InitialData:
    """Convert an initial_data dictionary from YAML/JSON to an InitialData object
    
    Args:
        initial_data_dict: Dictionary containing initial data definition
        
    Returns:
        InitialData object populated with rows from the dictionary
    """
    initial_data = InitialData(
        row_count=initial_data_dict.get('row_count', 0)
    )
    
    # Add rows
    for row_dict in initial_data_dict.get('rows', []):
        initial_data.add_row(row_dict)
        
    return initial_data 