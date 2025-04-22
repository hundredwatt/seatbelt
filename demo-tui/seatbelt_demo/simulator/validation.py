"""Validation operations for the Seatbelt Demo simulator."""

import hashlib
import json
import logging
from datetime import date, datetime
from typing import Any, Dict, List, Optional

from pyseatbelt import Source, Target, ValidationEngine

from ..validation.logic import Operation
from .transformations import Transformations
from .column_types import ColumnType
from .config import TRACING_IDS

# Custom JSON encoder to handle date and datetime objects
class CustomJSONEncoder(json.JSONEncoder):
    """Custom JSON encoder that can handle date and datetime objects."""
    
    def default(self, obj):
        if isinstance(obj, (datetime, date)):
            return obj.isoformat()
        return super().default(obj)

def format_target_for_validation(target_value: Any, target_type: Optional[ColumnType] = None) -> Any:
    # NOOP currently since our hash functions already sort JSON keys
    return target_value

class SimulatorSource(Source):
    """A source implementation for the simulator."""
    
    def __init__(self, database):
        self.database = database
        self.change_log_position = 0
        
    def read_change_log_changes(self, column_names: List[str]) -> Dict[Any, tuple]:
        """Read changes from the source change log.
        
        Args:
            column_names: List of column names to include in the change log
            
        Returns:
            Dictionary mapping row IDs to tuple of (source_signature, target_signature)
        """
        incremental_computation = {}
        
        while self.change_log_position < len(self.database.source_db_log):
            source_row = self.database.source_db_log[self.change_log_position].copy()
            self.change_log_position += 1

            # Calculate incremental checksums for INSERT and UPDATE operations
            if source_row.get('deleted', False):  # Skip DELETE operations
                continue

            del source_row['ts']
            del source_row['deleted']

            # Convert source row and values to target row and values
            target_row = source_row.copy()
            
            # Ensure fields that should NOT sync are removed
            for column in self.database.schema.columns:
                if (not column.sync_to_target and not column.target_only) and column.name in target_row:
                    del target_row[column.name]
            
            # Apply transformations based on target column types
            for column in self.database.schema.iter_target_columns():
                # Process target-only computed columns
                if getattr(column, 'target_only', False) and column.computed_from:
                    op_type = column.computed_from.get('operation')
                    arguments = column.computed_from.get('arguments', [])
                    source_columns = column.computed_from.get('source_columns', arguments)
                    
                    if op_type and source_columns:
                        # Collect values from source columns
                        source_values = []
                        for source_col in source_columns:
                            source_values.append(source_row.get(source_col))
                            
                        # Apply the operation using the Transformations class
                        computed_value = Transformations.apply_computed_operation(
                            op_type, source_values, arguments, column.target_type or column.type
                        )
                        target_row[column.name] = computed_value
                    continue
                
                # Apply type transformations to columns in target
                if column.name in target_row and target_row[column.name] is not None:
                    # Apply column type transformation using Transformations class
                    target_row[column.name] = Transformations.transform_source_to_target(
                        target_row[column.name], 
                        column.type, 
                        column.target_type
                    )
                    
            source_json = json.dumps(source_row, sort_keys=True, cls=CustomJSONEncoder)
            target_json = json.dumps(target_row, sort_keys=True, cls=CustomJSONEncoder)
            if source_row['id'] in TRACING_IDS:
                logging.info(f"[TRACE] SEATBELT CHECK: source_json={source_json}, target_json={target_json}")
            
            source_hash = hashlib.sha256(source_json.encode()).hexdigest()
            target_hash = hashlib.sha256(target_json.encode()).hexdigest() 
            
            incremental_computation[source_row['id']] = (source_hash, target_hash)
            
        return incremental_computation
    
    def read_signatures(self, column_names: List[str]) -> Dict[Any, Any]:
        """Read signatures (checksums) from the source.
        
        Args:
            column_names: List of column names to include in the signature
            
        Returns:
            Dictionary mapping row IDs to signatures
        """
        return {
            row['id']: hashlib.sha256(json.dumps({k: v for k, v in row.items() if k != 'ts' and k != 'deleted'}, 
                                                sort_keys=True, cls=CustomJSONEncoder).encode()).hexdigest()
            for row in self.database.source_db.values()
        }

class SimulatorTarget(Target):
    """A target implementation for the simulator."""
    
    def __init__(self, database):
        self.database = database
        
    def read_signatures(self, column_names: List[str]) -> Dict[Any, Any]:
        """Read signatures (checksums) from the target.
        
        Args:
            column_names: List of column names to include in the signature
            
        Returns:
            Dictionary mapping row IDs to signatures
        """
        target_db_signatures = {}
        for k, v in self.database.target_db.items():
            # Create a copy of target row to properly handle transformations
            target_row = v.copy()
            
            # Apply any final type-specific formatting to ensure consistent signatures
            for column in self.database.schema.columns:
                if column.name in target_row and target_row[column.name] is not None:
                    # Use the format_target_for_validation method
                    target_row[column.name] = format_target_for_validation(
                        target_row[column.name], 
                        column.target_type
                    )
            
            target_db_signatures[k] = hashlib.sha256(json.dumps(target_row, sort_keys=True, cls=CustomJSONEncoder).encode()).hexdigest()
            
        return target_db_signatures

class SimulationValidationEngine:
    """Adapter class to use pyseatbelt ValidationEngine with the simulator."""
    
    def __init__(self):
        self.engine = ValidationEngine()
        
    def seatbelt_check(self, database, metrics_tracker):
        """Validate data between source and target databases using pyseatbelt."""
        # Create source and target instances
        source = SimulatorSource(database)
        target = SimulatorTarget(database)
        
        # Run validation using pyseatbelt engine
        metrics = self.engine.seatbelt_check(source, target)
        
        # Update the local metrics tracker
        metrics_tracker.update(
            source_db_size=metrics['source_size'],
            target_db_size=metrics['target_size'],
            seatbelt_size=metrics['seatbelt_size'],
            error_count=metrics['error_count'],
            pending_count=metrics['pending_count'],
            valid_count=metrics['valid_count'],
        )
        
        return metrics_tracker.get() 