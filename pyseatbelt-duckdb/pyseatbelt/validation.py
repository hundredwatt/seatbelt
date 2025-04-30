"""Validation operations for the Seatbelt data integrity system."""

import json
import logging
import enum
from datetime import date, datetime
import duckdb
from duckdb.typing import *
import pandas as pd
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
            self.shadow = duckdb.connect()
            self.shadow.sql("""
                ATTACH '{}' AS persisted;
                COPY FROM DATABASE persisted TO memory;
                DETACH persisted
            """.format(shadow_file))
            self.initialize_shadow()
            logging.info(f"Seatbelt data loaded from {shadow_file}")
        else:
            self.shadow = duckdb.connect()
            self.initialize_shadow()
            logging.info(f"No seatbelt data provided")

    def save_shadow(self, file_path: str) -> bool:
        """Save the current shadow to a file.
        
        Args:
            file_path: Path to save the shadow data
            
        Returns:
            True if saving was successful, False otherwise
        """
        try:
            # Save the shadow DataFrame to a parquet file
            self.shadow.sql("""
                ATTACH '{}' AS persisted;
                COPY FROM DATABASE memory TO persisted;
                DETACH persisted
            """.format(file_path))
            logging.info(f"Seatbelt data saved to {file_path}")
            return True
        except Exception as e:
            logging.error(f"Failed to save seatbelt data to {file_path}: {str(e)}")
            return False
    
    def initialize_shadow(self):
        self.shadow.sql("""
            CREATE TABLE IF NOT EXISTS shadow (
                pk BIGINT PRIMARY KEY,
                source_signature BIGINT,
                target_signature BIGINT,
                incremental_source_signature BIGINT,
                incremental_target_signature BIGINT,
                source_operation UTINYINT,
                target_operation UTINYINT,
                validation_error BOOLEAN
            )
        """)
        # Register UDFs
        # Create a wrapper function that returns an integer instead of an Operation enum
        def determine_source_operation_wrapper(old_signature, new_signature):
            operation = determine_source_operation(old_signature, new_signature)
            # Convert the Operation enum to its integer value
            return operation.value if operation is not None else None
        
        def check_for_validation_error_wrapper(source_operation_value,
                             previous_source_operation_value,
                             destination_operation_value,
                             previous_destination_operation_value,
                             existing_validation_error,
                             row_verified):

            source_operation = UTINYINT_TO_OPERATION[source_operation_value]
            previous_source_operation = UTINYINT_TO_OPERATION[previous_source_operation_value]
            destination_operation = UTINYINT_TO_OPERATION[destination_operation_value]
            previous_destination_operation = UTINYINT_TO_OPERATION[previous_destination_operation_value]
            return check_for_validation_error(source_operation, previous_source_operation, destination_operation, previous_destination_operation, existing_validation_error, row_verified)
            
        self.shadow.create_function('determine_source_operation', determine_source_operation_wrapper, 
                          [BIGINT, BIGINT], UTINYINT, null_handling="special")
        self.shadow.create_function('verify_row_integrity_from_incremental_checksums', verify_row_integrity_from_incremental_checksums, 
                          [BIGINT, BIGINT, BIGINT, BIGINT], BOOLEAN, null_handling="special")
        self.shadow.create_function('check_for_validation_error', check_for_validation_error_wrapper, 
                          [UTINYINT, UTINYINT, UTINYINT, UTINYINT, BOOLEAN, BOOLEAN], BOOLEAN, null_handling="special")
        
        def validation_status(source_operation_value, target_operation_value, incremental_match, validation_error):
            if validation_error:
                return ValidationStatus.ERROR.value

            source_operation = UTINYINT_TO_OPERATION[source_operation_value]
            target_operation = UTINYINT_TO_OPERATION[target_operation_value]

            pending = source_operation not in [Operation.NOOP, Operation.DOES_NOT_EXIST] and target_operation in [Operation.NOOP, Operation.DOES_NOT_EXIST]
            pending |= not incremental_match and source_operation not in [Operation.DOES_NOT_EXIST, Operation.DELETE]
            pending |= source_operation in [Operation.DOES_NOT_EXIST, Operation.DELETE] and target_operation not in [Operation.DOES_NOT_EXIST, Operation.DELETE]

            gone = source_operation in [Operation.DOES_NOT_EXIST, Operation.DELETE] and target_operation in [Operation.DOES_NOT_EXIST, Operation.DELETE]

            if gone:
                return ValidationStatus.GONE.value
            elif pending:
                return ValidationStatus.PENDING.value
            else:
                return ValidationStatus.VALID.value

        self.shadow.create_function('validation_status', validation_status, [UTINYINT, UTINYINT, BOOLEAN, BOOLEAN], UTINYINT, null_handling="special")

    # For testing purposes
    def fetchall_shadow(self):
        results = {}
        column_names = [
            'pk',
            'source_signature',
            'target_signature',
            'incremental_source_signature',
            'incremental_target_signature',
            'source_operation',
            'target_operation',
            'validation_error',
        ]
        for row in self.shadow.sql("SELECT * FROM shadow").fetchall():
            results[row[0]] = dict(zip(column_names[1:], row[1:]))
        return results


    def seatbelt_check(self, source: Source, target: Target, column_names: List[str] = None, 
                       partitions: Optional[int] = None, current_partition: Optional[int] = None,
                       id_range: Optional[Tuple[Optional[int], Optional[int]]] = None):
        """Validate data between source and target.
        
        Args:
            source: Source instance to validate from
            target: Target instance to validate against
            column_names: Optional list of column names to include in validation
            partitions: Optional number of partitions to use
            current_partition: Optional current partition number (0-based)
            id_range: Optional tuple of (min_id, max_id) to limit the validation range
            
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

        # Convert dictionaries to pandas DataFrames before registering with DuckDB
        incremental_computation_flattned = [[k, v[0], v[1]] for k, v in incremental_computation.items()]
        incremental_df = pd.DataFrame(incremental_computation_flattned, columns=['pk', 'source_signature', 'target_signature'])
        source_df = pd.DataFrame(source_db_signatures.items(), columns=['pk', 'source_signature'])
        target_df = pd.DataFrame(target_db_signatures.items(), columns=['pk', 'target_signature'])
        
        # Connect to DuckDB and register DataFrames
        self.shadow.register('incremental_computation', incremental_df)
        self.shadow.register('source_db_signatures', source_df)
        self.shadow.register('target_db_signatures', target_df)

        
        # Run update query
        def where_clause(table_name):
            if partitions is not None:
                return " {}.pk % {} = {}".format(table_name, partitions, current_partition)
            if id_range is not None:
                min_id, max_id = id_range
                conditions = []
                if min_id is not None:
                    conditions.append("{}.pk >= {}".format(table_name, min_id))
                if max_id is not None:
                    conditions.append("{}.pk < {}".format(table_name, max_id))
                return " AND ".join(conditions) if conditions else "1=1"
            return " 1=1"

        self.shadow.sql("""
            INSERT INTO shadow
            SELECT
                COALESCE(source.pk, target.pk, incremental_computation.pk, shadow.pk) AS pk,
                source.source_signature AS source_signature,
                target.target_signature AS target_signature,
                incremental_computation.source_signature AS incremental_source_signature,
                incremental_computation.target_signature AS incremental_target_signature,
                determine_source_operation(
                    source.source_signature,
                    shadow.source_signature
                ) AS source_operation,
                determine_source_operation(
                    target.target_signature,
                    shadow.target_signature
                ) AS target_operation,
                check_for_validation_error(
                    determine_source_operation(
                        source.source_signature,
                        shadow.source_signature
                    ),
                    shadow.source_operation,
                    determine_source_operation(
                        target.target_signature,
                        shadow.target_signature
                    ),
                    shadow.target_operation,
                    shadow.validation_error,
                    verify_row_integrity_from_incremental_checksums(
                        incremental_computation.source_signature,
                        incremental_computation.target_signature,
                        source.source_signature,
                        target.target_signature
                    )
                ) AS validation_error
            FROM source_db_signatures source
            FULL OUTER JOIN shadow ON source.pk = shadow.pk AND {}
            FULL OUTER JOIN target_db_signatures target ON COALESCE(source.pk, shadow.pk) = target.pk AND {}
            FULL OUTER JOIN incremental_computation ON COALESCE(source.pk, shadow.pk, target.pk) = incremental_computation.pk AND {}
            WHERE {}
            ORDER BY pk
            ON CONFLICT (pk) DO UPDATE SET
                source_signature = excluded.source_signature,
                target_signature = excluded.target_signature,
                incremental_source_signature = excluded.incremental_source_signature,
                incremental_target_signature = excluded.incremental_target_signature,
                source_operation = excluded.source_operation,
                target_operation = excluded.target_operation,
                validation_error = excluded.validation_error;
        """.format(where_clause('shadow'), where_clause('target'), where_clause('incremental_computation'), where_clause('source')))

        # Clean up gone rows
        self.shadow.sql("""
            DELETE FROM shadow
            WHERE 
                validation_status(
                        source_operation,
                        target_operation,
                        verify_row_integrity_from_incremental_checksums(
                            incremental_source_signature,
                            incremental_target_signature,
                            source_signature,
                            target_signature
                        ),
                        validation_error
                ) = 3
        """)

        # Update metrics
        metrics_df = self.shadow.sql("""
            SELECT
                COUNT(source_signature) FILTER (WHERE source_signature IS NOT NULL) AS source_size,
                COUNT(target_signature) FILTER (WHERE target_signature IS NOT NULL) AS target_size,
                COUNT(*) AS seatbelt_size,
                COUNT(validation_error) FILTER (WHERE validation_error = TRUE) AS error_count,
                COUNT(validation_status) FILTER (WHERE validation_status = 1) AS pending_count,
                COUNT(validation_status) FILTER (WHERE validation_status = 0) AS valid_count,
            FROM (
                SELECT
                    *,
                    validation_status(
                            source_operation,
                            target_operation,
                            verify_row_integrity_from_incremental_checksums(
                                incremental_source_signature,
                                incremental_target_signature,
                                source_signature,
                                target_signature
                            ),
                            validation_error
                    ) AS validation_status
                FROM shadow
            )
        """).df()
        
        self.metrics.update(
            source_size=metrics_df.iloc[0]['source_size'],
            target_size=metrics_df.iloc[0]['target_size'],
            seatbelt_size=metrics_df.iloc[0]['seatbelt_size'],
            error_count=metrics_df.iloc[0]['error_count'],
            pending_count=metrics_df.iloc[0]['pending_count'],
            valid_count=metrics_df.iloc[0]['valid_count'],    
        )

        return self.metrics