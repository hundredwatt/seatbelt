"""Validation operations for the Seatbelt data integrity system."""

import json
import logging
import enum
from datetime import date, datetime
from typing import Any, Optional, List, Tuple, Dict
from abc import ABC, abstractmethod

# Import from reference directory directly
import sys
import os
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', '..', 'reference'))
from validation_logic import (
    Operation,
    UTINYINT_TO_OPERATION,
    determine_source_operation,
    verify_row_integrity_from_incremental_checksums,
    check_for_validation_error
)

from .config import TRACING_IDS

def format_target_for_validation(target_value: Any, target_type: Optional[str] = None) -> Any:
    # NOOP currently since our hash functions already sort JSON keys
    return target_value

# Custom JSON encoder to handle date and datetime objects
class CustomJSONEncoder(json.JSONEncoder):
    """Custom JSON encoder that can handle date and datetime objects and Operation enums."""

    def default(self, obj):
        if isinstance(obj, (datetime, date)):
            return obj.isoformat()
        # Add handling for Operation enum values
        if hasattr(obj, '__class__') and obj.__class__.__name__ == 'Operation':
            return {"__operation__": obj.value}
        # Add handling for ValidationStatus enum values
        if isinstance(obj, ValidationStatus):
            return {"__validation_status__": obj.value}
        return super().default(obj)

# Custom object hook for JSON decoding to handle Operation enums and convert numeric keys
def custom_json_decoder(obj):
    # Handle numeric keys if this is the top-level object
    if isinstance(obj, dict):
        # Convert string keys to integers when they represent numbers
        result = {}
        for key, value in obj.items():
            # Try to convert the key to int if it's a digit string
            if isinstance(key, str) and key.isdigit():
                result[int(key)] = value
            else:
                result[key] = value
                
        # Handle Operation enum values
        if len(result) == 1 and "__operation__" in result:
            try:
                return UTINYINT_TO_OPERATION[result["__operation__"]]
            except (KeyError, TypeError):
                # If we can't convert it, just return the original object
                return result
        
        # Handle ValidationStatus enum values
        if len(result) == 1 and "__validation_status__" in result:
            try:
                return ValidationStatus(result["__validation_status__"])
            except (KeyError, TypeError, ValueError):
                # If we can't convert it, just return the original object
                return result
        
        return result
    return obj

class Source(ABC):
    """Abstract base class for data sources.
    
    Implementations should provide methods to read change logs and signatures.
    """
    @abstractmethod
    def read_change_log_changes(self, column_names: List[str]) -> Dict[Any, Tuple[Any, Any]]:
        """Read changes from the source change log.
        
        Args:
            column_names: List of column names to include in the change log
            
        Returns:
            Dictionary mapping row IDs to tuple of (source_signature, target_signature)
        """
        pass

    @abstractmethod
    def read_signatures(self, column_names: List[str]) -> Dict[Any, Any]:
        """Read signatures (checksums) from the source.
        
        Args:
            column_names: List of column names to include in the signature
            
        Returns:
            Dictionary mapping row IDs to signatures
        """
        pass

class Target(ABC):
    """Abstract base class for data targets.
    
    Implementations should provide methods to read signatures.
    """
    @abstractmethod
    def read_signatures(self, column_names: List[str]) -> Dict[Any, Any]:
        """Read signatures (checksums) from the target.
        
        Args:
            column_names: List of column names to include in the signature
            
        Returns:
            Dictionary mapping row IDs to signatures
        """
        pass

class ValidationStatus(enum.Enum):
    """Enum for validation status."""
    VALID = 0
    PENDING = 1
    ERROR = 2
    GONE = 3

    def __str__(self):
        return self.name

class ValidationEngine:
    """Class responsible for data validation between source and target."""

    def __init__(self, shadow_file: Optional[str] = None):
        self.shadow = {}
        self.metrics = {
            'source_size': 0,
            'target_size': 0,
            'seatbelt_size': 0,
            'error_count': 0,
            'pending_count': 0,
            'valid_count': 0,
        }
        
        # Load shadow from file if provided
        if shadow_file and os.path.exists(shadow_file):
            try:
                with open(shadow_file, 'r') as f:
                    # Our custom_json_decoder will handle numeric keys and Operation enums
                    self.shadow = json.load(f, object_hook=custom_json_decoder)
                logging.info(f"Seatbelt data loaded")
            except Exception as e:
                logging.error(f"Failed to load shadow from {shadow_file}: {str(e)}")

    def save_shadow(self, file_path: str) -> bool:
        """Save the current shadow to a file.
        
        Args:
            file_path: Path to save the shadow data
            
        Returns:
            True if saving was successful, False otherwise
        """
        try:
            with open(file_path, 'w') as f:
                json.dump(self.shadow, f, cls=CustomJSONEncoder, indent=2)
            logging.info(f"Seatbelt data saved")
            return True
        except Exception as e:
            logging.error(f"Failed to save seatbelt data to {file_path}: {str(e)}")
            return False

    def seatbelt_check(self, source: Source, target: Target, column_names: List[str] = None):
        """Validate data between source and target.
        
        Args:
            source: Source instance to validate from
            target: Target instance to validate against
            column_names: Optional list of column names to include in validation
            
        Returns:
            Dictionary containing validation metrics
        """
        if column_names is None:
            column_names = []
            
        # 1. Update the incremental computation based on change log entries
        incremental_computation = source.read_change_log_changes(column_names)

        # 2. Read the source signatures
        source_db_signatures = source.read_signatures(column_names)

        # 3. Read the target signatures
        target_db_signatures = target.read_signatures(column_names)

        # 4. Update the shadow (seatbelt)
        ids = set(source_db_signatures.keys()) | \
            set(target_db_signatures.keys()) | \
            set(incremental_computation.keys()) | \
            set(self.shadow.keys())

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
            seatbelt_row = self.shadow.get(id, {})

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
            
            status = None
            if error:
                status = ValidationStatus.ERROR
            elif not error:
                pending = source_operation not in [Operation.NOOP, Operation.DOES_NOT_EXIST] and target_operation in [Operation.NOOP, Operation.DOES_NOT_EXIST]
                pending |= not incremental_match and source_operation not in [Operation.DOES_NOT_EXIST, Operation.DELETE]
                pending |= source_operation in [Operation.DOES_NOT_EXIST, Operation.DELETE] and target_operation not in [Operation.DOES_NOT_EXIST, Operation.DELETE]

                gone = source_operation in [Operation.DOES_NOT_EXIST, Operation.DELETE] and target_operation in [Operation.DOES_NOT_EXIST, Operation.DELETE]

                if gone:
                    status = ValidationStatus.GONE
                elif pending:
                    status = ValidationStatus.PENDING
                else:
                    status = ValidationStatus.VALID

            if id in TRACING_IDS:
                logging.info(f"[TRACE] SEATBELT CHECK: id={id}, validation_status={status}, source_operation={source_operation}, previous_source_operation={previous_source_operation}, target_operation={target_operation}, previous_target_operation={previous_target_operation}, previous_error={previous_error}, error={error}, incremental_match={incremental_match}")

            if status == ValidationStatus.GONE:
                if id in self.shadow:
                    del self.shadow[id]
                continue

            self.shadow[id] = {
                'source_signature': source_signature,
                'target_signature': target_signature,
                'incremental_source_signature': incremental_hashes[0],
                'incremental_target_signature': incremental_hashes[1],
                'source_operation': source_operation,
                'target_operation': target_operation,
                'validation_error': error,
                'validation_status': status,
            }

            match status:
                case ValidationStatus.VALID:
                    valid_count += 1
                case ValidationStatus.PENDING:
                    pending_count += 1
                case ValidationStatus.ERROR:
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


        # Update metrics
        self.metrics.update(
            source_size=len(source_db_signatures),
            target_size=len(target_db_signatures),
            seatbelt_size=len(self.shadow),
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

        return self.metrics
