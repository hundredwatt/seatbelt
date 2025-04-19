"""Core validation logic for the Seatbelt Demo - imports from reference implementation."""

import sys
import os
from pathlib import Path

# Add the parent directory to sys.path to import the reference file
root_dir = Path(__file__).parent.parent.parent.parent
reference_path = os.path.join(root_dir, "reference")
sys.path.insert(0, str(reference_path))

# Import everything from the reference validation_logic module
from validation_logic import (
    Operation,
    DOES_NOT_EXIST,
    NOOP,
    INSERT,
    UPDATE,
    DELETE,
    INSERT_AND_UPDATE,
    UPDATE_AND_DELETE,
    TRANSIENT_UPDATE,
    UTINYINT_TO_OPERATION,
    operation_from_int,
    determine_source_operation,
    determine_destination_operation,
    check_for_validation_error,
    verify_row_integrity_from_incremental_checksums
)

# Re-export everything
__all__ = [
    'Operation',
    'DOES_NOT_EXIST',
    'NOOP',
    'INSERT',
    'UPDATE',
    'DELETE',
    'INSERT_AND_UPDATE',
    'UPDATE_AND_DELETE',
    'TRANSIENT_UPDATE',
    'UTINYINT_TO_OPERATION',
    'operation_from_int',
    'determine_source_operation',
    'determine_destination_operation',
    'check_for_validation_error',
    'verify_row_integrity_from_incremental_checksums'
] 