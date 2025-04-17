"""Validation operations for the Seatbelt Demo simulator."""

import hashlib
import json
import logging
from datetime import date, datetime

from ..validation.logic import (
    Operation,
    determine_source_operation,
    check_for_validation_error
)

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
    
    def seatbelt_check(self, database, etl_processor, metrics_tracker):
        """Validate data between source and target databases"""
        source_db_signatures = {
            row['id']: row['ts'] for row in database.source_db.values()
        }
        target_db_signatures = {
            k: hashlib.sha256(json.dumps(v, sort_keys=True, cls=CustomJSONEncoder).encode()).hexdigest() 
            for k, v in database.target_db.items()
        }
        
        ids = set(source_db_signatures.keys()) | \
            set(target_db_signatures.keys()) | \
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
            
            source_operation = determine_source_operation(source_signature, seatbelt_row.get('source_signature', None))
            target_operation = determine_source_operation(target_signature, seatbelt_row.get('target_signature', None))
            previous_source_operation = seatbelt_row.get('source_operation', None)
            previous_target_operation = seatbelt_row.get('target_operation', None)
            previous_error = seatbelt_row.get('validation_error', False)
            
            # Check NULL equivalence between source and target rows for all nullable columns
            null_mismatch = False
            if source_signature is not None and target_signature is not None:
                source_row = database.source_db.get(id, {})
                target_row = database.target_db.get(id, {})
                
                # Compare all nullable columns' NULL state
                for column in database.schema.columns:
                    if column.nullable and column.name in source_row and column.name in target_row:
                        source_is_null = source_row[column.name] is None
                        target_is_null = target_row[column.name] is None
                        if source_is_null != target_is_null:
                            null_mismatch = True
                            break
                        
            # A row is considered to have an error when there's a NULL mismatch or validation error
            if null_mismatch and source_operation == Operation.NOOP and target_operation == Operation.NOOP:
                error = True
            else:
                error = check_for_validation_error(
                    source_operation,
                    previous_source_operation,
                    target_operation,
                    previous_target_operation,
                    previous_error
                )
                
            if id in etl_processor.tracing_ids:
                logging.info(f"[TRACE] SEATBELT CHECK: id={id}, source_operation={source_operation}, previous_source_operation={previous_source_operation}, target_operation={target_operation}, previous_target_operation={previous_target_operation}, previous_error={previous_error}, error={error}, null_mismatch={null_mismatch}")
                
            self.seatbelt[id] = {
                'source_signature': source_signature,
                'target_signature': target_signature,
                'source_operation': source_operation,
                'target_operation': target_operation,
                'validation_error': error,
                'null_mismatch': null_mismatch
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
                    
                logging.debug(f"Validation error for id={id} persists")
                logging.debug(f"seatbelt error: id={id}, source_operation={source_operation}, previous_source_operation={previous_source_operation}, target_operation={target_operation}, previous_target_operation={previous_target_operation}, previous_error={previous_error}, error={error}, null_mismatch={null_mismatch}")
            elif source_operation not in [Operation.NOOP, Operation.DOES_NOT_EXIST] and target_operation in [Operation.NOOP, Operation.DOES_NOT_EXIST]:
                # Count rows that are present in source but not yet in target_db (or have a different state)
                pending_count += 1
            elif null_mismatch:
                # Count NULL mismatches as pending
                pending_count += 1
            elif source_signature is not None and target_signature is not None:
                # Count rows that are present in both source and target_db and have no errors
                valid_count += 1
                
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