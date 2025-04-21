"""Validation operations for the Seatbelt Demo simulator."""

import hashlib
import json
import logging
from datetime import date, datetime
from typing import Any, Optional

from ..validation.logic import (
    Operation,
    determine_source_operation,
    verify_row_integrity_from_incremental_checksums,
    check_for_validation_error
)
from .transformations import Transformations
from .column_types import ColumnType
from .config import TRACING_IDS

def format_target_for_validation(target_value: Any, target_type: Optional[ColumnType] = None) -> Any:
    # NOOP currently since our hash functions already sort JSON keys
    return target_value

# Custom JSON encoder to handle date and datetime objects
class CustomJSONEncoder(json.JSONEncoder):
    """Custom JSON encoder that can handle date and datetime objects."""
    
    def default(self, obj):
        if isinstance(obj, (datetime, date)):
            return obj.isoformat()
        return super().default(obj)

class ValidationEngine:
    """Class responsible for data validation"""
    
    def __init__(self):
        self.seatbelt = {}
        self.change_log_position = 0
        
    def seatbelt_check(self, database, metrics_tracker):
        """Validate data between source and target databases"""
        # 1. Update the incremental computation based on change log entries
        incremental_computation = {}
        while self.change_log_position < len(database.source_db_log):
            source_row = database.source_db_log[self.change_log_position].copy()
            self.change_log_position += 1

            # Calculate incremental checksums for INSERT and UPDATE operations
            if source_row.get('deleted', False):  # INSERT and UPDATE operations
                continue

            del source_row['ts']
            del source_row['deleted']

            # Convert source row and values to target row and values
            target_row = source_row.copy()
            
            # Ensure fields that should NOT sync are removed
            for column in database.schema.columns:
                if (not column.sync_to_target and not column.target_only) and column.name in target_row:
                    del target_row[column.name]
            
            # Apply transformations based on target column types
            for column in database.schema.iter_target_columns():
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
            
            
        # 2. Read the source signatures
        source_db_signatures = {
            row['id']: hashlib.sha256(json.dumps({k: v for k, v in row.items() if k != 'ts' and k != 'deleted'}, sort_keys=True, cls=CustomJSONEncoder).encode()).hexdigest()
            for row in database.source_db.values()
        }
        
        # 3. Read the target signatures
        target_db_signatures = {}
        for k, v in database.target_db.items():
            # Create a copy of target row to properly handle transformations
            target_row = v.copy()
            
            # Apply any final type-specific formatting to ensure consistent signatures
            for column in database.schema.columns:
                if column.name in target_row and target_row[column.name] is not None:
                    # Use the format_target_for_validation method from Transformations
                    target_row[column.name] = format_target_for_validation(
                        target_row[column.name], 
                        column.target_type
                    )
            
            target_db_signatures[k] = hashlib.sha256(json.dumps(target_row, sort_keys=True, cls=CustomJSONEncoder).encode()).hexdigest()
        
        # 4. Update the shadow (seatbelt)
        ids = set(source_db_signatures.keys()) | \
            set(target_db_signatures.keys()) | \
            set(incremental_computation.keys()) | \
            set(self.seatbelt.keys())
            
        error_count = 0
        pending_count = 0
        valid_count = 0
        
        # Categorize discrepant IDs for more detailed reporting
        source_only_ids = []
        target_only_ids = []
        stale_ids = []
        
        for id in ids:
            source_signature = source_db_signatures.get(id, None)
            target_signature = target_db_signatures.get(id, None)
            seatbelt_row = self.seatbelt.get(id, {})
            
            # Get incremental hashes or reuse previous ones if not in current incremental computation
            incremental_hashes = incremental_computation.get(
                id, 
                (seatbelt_row.get('incremental_source_signature', None), 
                 seatbelt_row.get('incremental_target_signature', None))
            )
            
            source_operation = determine_source_operation(source_signature, seatbelt_row.get('source_signature', None))
            target_operation = determine_source_operation(target_signature, seatbelt_row.get('target_signature', None))
            previous_source_operation = seatbelt_row.get('source_operation', None)
            previous_target_operation = seatbelt_row.get('target_operation', None)
            previous_error = seatbelt_row.get('validation_error', False)
            
            incremental_match = verify_row_integrity_from_incremental_checksums(
                incremental_hashes[0],
                incremental_hashes[1],
                source_signature,
                target_signature
            )
            
            error = check_for_validation_error(
                source_operation,
                previous_source_operation,
                target_operation,
                previous_target_operation,
                previous_error,
                incremental_match
            )
            pending = False
            if not error:
                pending = source_operation not in [Operation.NOOP, Operation.DOES_NOT_EXIST] and target_operation in [Operation.NOOP, Operation.DOES_NOT_EXIST]
                pending |= not incremental_match and source_operation not in [Operation.DOES_NOT_EXIST, Operation.DELETE]

                
            # Check for duplication of rows (multiple entries with same ID)
            source_duplication = sum(1 for row in database.source_db.values() if row['id'] == id) > 1
            target_duplication = sum(1 for row_id in database.target_db.keys() if row_id == id) > 1
            
            if source_duplication or target_duplication:
                error = True
                
                
            self.seatbelt[id] = {
                'source_signature': source_signature,
                'target_signature': target_signature,
                'incremental_source_signature': incremental_hashes[0],
                'incremental_target_signature': incremental_hashes[1],
                'source_operation': source_operation,
                'target_operation': target_operation,
                'validation_error': error,
            }
            
            if error:
                error_count += 1
                # Categorize the error
                if source_signature is not None and target_signature is None:
                    # Exists in source but not in target
                    source_only_ids.append(id)
                elif source_signature is None and target_signature is not None:
                    # Exists in target but not in source
                    target_only_ids.append(id)
                else:
                    # Other validation errors (stale)
                    stale_ids.append(id)
            elif pending:
                pending_count += 1
            if not pending and not error and source_signature is not None and target_signature is not None:
                # Count rows that are present in both source and target_db and have no errors
                valid_count += 1
                
            if id in TRACING_IDS:
                status = "VALID" if not pending and not error else "PENDING" if pending else "ERROR"
                logging.info(f"[TRACE] SEATBELT CHECK: id={id}, status={status}, source_operation={source_operation}, previous_source_operation={previous_source_operation}, target_operation={target_operation}, previous_target_operation={previous_target_operation}, previous_error={previous_error}, error={error}, incremental_match={incremental_match}")
                
        # Update metrics
        metrics_tracker.update(
            source_db_size=len(database.source_db),
            target_db_size=len(database.target_db),
            seatbelt_size=len(self.seatbelt),
            error_count=error_count,
            pending_count=pending_count,
            valid_count=valid_count,
        )
        
        # Display categorized errors as debug messages
        if error_count > 0:
            if source_only_ids:
                source_only_str = ", ".join(str(id) for id in source_only_ids[:5])
                if len(source_only_ids) > 5:
                    source_only_str += f" (and {len(source_only_ids) - 5} more)"
                logging.debug(f"Source-Only Rows: {source_only_str}")
                
            if target_only_ids:
                target_only_str = ", ".join(str(id) for id in target_only_ids[:5])
                if len(target_only_ids) > 5:
                    target_only_str += f" (and {len(target_only_ids) - 5} more)"
                logging.debug(f"Target-Only Rows: {target_only_str}")
                
            if stale_ids:
                stale_str = ", ".join(str(id) for id in stale_ids[:5])
                if len(stale_ids) > 5:
                    stale_str += f" (and {len(stale_ids) - 5} more)"
                logging.debug(f"Drifted Rows: {stale_str}")
        
        # Log just the important metrics with a clean format
        logging.info(f"SEATBELT CHECK: valid={valid_count}, in-flight={pending_count}, discrepant={error_count}")
        
        return metrics_tracker.get() 