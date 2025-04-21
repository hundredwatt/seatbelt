"""Configuration loading for the Seatbelt Demo simulator."""

import json
import yaml
import logging
from pathlib import Path
from typing import Dict, List, Any, Optional, Union, Callable
from dataclasses import asdict

from .column_types import ColumnType
from .database import SchemaDefinition, ColumnDefinition, InitialData
from .schema_utils import convert_schema_dict, convert_initial_data_dict

class ConfigurationError(Exception):
    """Exception raised for configuration errors."""
    pass

def load_config_file(file_path: Union[str, Path]) -> Dict[str, Any]:
    """Load a configuration file (YAML or JSON)."""
    file_path = Path(file_path)
    
    if not file_path.exists():
        raise ConfigurationError(f"Configuration file not found: {file_path}")
    
    try:
        if file_path.suffix.lower() in ('.yaml', '.yml'):
            with open(file_path, 'r') as f:
                return yaml.safe_load(f)
        elif file_path.suffix.lower() == '.json':
            with open(file_path, 'r') as f:
                return json.load(f)
        else:
            raise ConfigurationError(f"Unsupported configuration file format: {file_path.suffix}")
    except Exception as e:
        raise ConfigurationError(f"Error loading configuration file: {e}")

def create_schema_from_config(config: Dict[str, Any]) -> SchemaDefinition:
    """Create a schema definition from a configuration dict."""
    if 'schema' not in config:
        return None
    
    try:
        return convert_schema_dict(config['schema'])
    except Exception as e:
        raise ConfigurationError(f"Error creating schema from configuration: {e}")

def create_initial_data_from_config(config: Dict[str, Any]) -> Optional[InitialData]:
    """Create initial data configuration from a configuration dict."""
    if 'initial_data' not in config:
        return None  # Return None if not specified in config

    try:
        return convert_initial_data_dict(config['initial_data'])
    except Exception as e:
        raise ConfigurationError(f"Error creating initial data from configuration: {e}")

def load_simulator_config(file_path: Union[str, Path]) -> Dict[str, Any]:
    """Load the full simulator configuration."""
    config = load_config_file(file_path)
    
    result = {
        'schema': create_schema_from_config(config),
        'initial_data': create_initial_data_from_config(config),
        'random_seed': config.get('random_seed', 42),
        'seatbelt_interval': config.get('seatbelt_interval', 25),
    }
    
    logging.info(f"Loaded configuration from {file_path}")
    return result

def save_config_to_file(config: Dict[str, Any], file_path: Union[str, Path]) -> None:
    """Save a configuration to a file (YAML or JSON)."""
    file_path = Path(file_path)
    
    # Convert schema to dict if it's a SchemaDefinition
    if 'schema' in config and isinstance(config['schema'], SchemaDefinition):
        schema_dict = {
            'columns': []
        }
        
        for column in config['schema'].columns:
            # Skip id column as it's added automatically
            if column.name == 'id':
                continue
                
            column_dict = {
                'name': column.name,
                'type': column.type.value,
                'nullable': column.nullable
            }
            
            if column.target_type:
                column_dict['target_type'] = column.target_type.value
                
            schema_dict['columns'].append(column_dict)
            
        config_to_save = config.copy()
        config_to_save['schema'] = schema_dict
    else:
        config_to_save = config
    
    # Convert initial data if it's an InitialData object
    if 'initial_data' in config and isinstance(config['initial_data'], InitialData):
        initial_data_dict = {
            'row_count': config['initial_data'].row_count,
            'rows': config['initial_data'].rows
        }
        config_to_save['initial_data'] = initial_data_dict
    
    try:
        if file_path.suffix.lower() in ('.yaml', '.yml'):
            with open(file_path, 'w') as f:
                yaml.dump(config_to_save, f, default_flow_style=False)
        elif file_path.suffix.lower() == '.json':
            with open(file_path, 'w') as f:
                json.dump(config_to_save, f, indent=2)
        else:
            raise ConfigurationError(f"Unsupported configuration file format: {file_path.suffix}")
            
        logging.info(f"Saved configuration to {file_path}")
    except Exception as e:
        raise ConfigurationError(f"Error saving configuration file: {e}")

def get_default_config() -> Dict[str, Any]:
    """Get a default configuration."""
    # Removed schema definition here - Database class provides its own default
    
    return {
        'initial_data': InitialData(),
        'random_seed': 42,
        'seatbelt_interval': 25
    } 