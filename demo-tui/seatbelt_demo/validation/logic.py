"""Core validation logic for the Seatbelt Demo."""

import enum

# Operation classifications
class Operation(enum.Enum):
    DOES_NOT_EXIST = enum.auto()
    NOOP = enum.auto()
    INSERT = enum.auto()
    UPDATE = enum.auto()
    DELETE = enum.auto()
    INSERT_AND_UPDATE = enum.auto()
    UPDATE_AND_DELETE = enum.auto()
    TRANSIENT_UPDATE = enum.auto()

DOES_NOT_EXIST = Operation.DOES_NOT_EXIST
NOOP = Operation.NOOP
INSERT = Operation.INSERT
UPDATE = Operation.UPDATE
DELETE = Operation.DELETE
INSERT_AND_UPDATE = Operation.INSERT_AND_UPDATE
UPDATE_AND_DELETE = Operation.UPDATE_AND_DELETE
TRANSIENT_UPDATE = Operation.TRANSIENT_UPDATE

UTINYINT_TO_OPERATION = {e.value : e for e in Operation}
UTINYINT_TO_OPERATION[None] = None

def operation_from_int(value: int):
    return UTINYINT_TO_OPERATION[value]

# Core functions
# arguments can be any type that can be compared for equality
def determine_source_operation(checksum_1: any, checksum_0: any) -> Operation:
    if checksum_1 is None and checksum_0 is None:
        return DOES_NOT_EXIST
    if checksum_1 is not None and checksum_0 is None:
        return INSERT
    if checksum_1 is None and checksum_0 is not None:
        return DELETE
    if checksum_1 is not None and checksum_0 is not None and checksum_1 != checksum_0:
        return UPDATE
    return NOOP

def determine_destination_operation(destination_present_end: bool, destination_updated: bool, destination_present_start: bool) -> Operation:
    match destination_present_end, destination_updated, destination_present_start:
        case True, True, True:
            return UPDATE
        case True, True, False:
            return INSERT_AND_UPDATE
        case True, False, True:
            return NOOP
        case True, False, False:
            return INSERT
        case False, True, True:
            return UPDATE_AND_DELETE
        case False, True, False:
            return TRANSIENT_UPDATE
        case False, False, True:
            return DELETE
        case False, False, False:
            return DOES_NOT_EXIST

def check_for_validation_error(source_operation: Operation,
                             previous_source_operation: Operation,
                             destination_operation: Operation,
                             previous_destination_operation: Operation,
                             existing_validation_error: bool) -> bool:
    # A source operation occured in the previous iteration, but no changes were seen at destination
    # NOTE: TRANSIENT_UPDATEs in the destination cause this rule to not apply
    if previous_source_operation not in [NOOP, DOES_NOT_EXIST, DELETE] \
        and previous_destination_operation in [NOOP, DOES_NOT_EXIST] \
        and destination_operation in [NOOP, DOES_NOT_EXIST] \
        and source_operation in [NOOP, DOES_NOT_EXIST]:
        return True

    # A row exists in the source, but not in the destination
    if source_operation in [NOOP, UPDATE] \
    and previous_source_operation in [NOOP, UPDATE, INSERT] \
    and destination_operation == DOES_NOT_EXIST:
        return True

    # A row exists in the destination, but not in the source
    if source_operation == DOES_NOT_EXIST \
    and previous_source_operation == DOES_NOT_EXIST \
    and destination_operation == NOOP:
        return True

    # Corrupted destination - destination shows changes but source is unchanged
    if source_operation == NOOP \
    and previous_source_operation == NOOP \
    and destination_operation != NOOP:
        return True

    # There was previously a validation error, and no changes have happened in source or destination
    if existing_validation_error:
        if source_operation in [NOOP, DOES_NOT_EXIST] and destination_operation in [NOOP, DOES_NOT_EXIST]:
            return True

    return False 