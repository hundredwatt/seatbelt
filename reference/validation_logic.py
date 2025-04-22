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

def verify_row_integrity_from_incremental_checksums(
        incremental_source_checksum: any,
        incremental_destination_checksum: any,
        source_checksum: any,
        destination_checksum: any,
        ) -> bool:
    if incremental_source_checksum is None or incremental_destination_checksum is None:
        return True
    if incremental_source_checksum == source_checksum and incremental_destination_checksum == destination_checksum:
        return True
    return False

def check_for_validation_error(source_operation: Operation,
                             previous_source_operation: Operation,
                             destination_operation: Operation,
                             previous_destination_operation: Operation,
                             existing_validation_error: bool,
                             row_verified: bool = True) -> bool:
    # A source operation besides DELETE occured in the previous iteration, but no changes were seen at destination
    # NOTE: TRANSIENT_UPDATEs in the destination cause this rule to not apply
    if previous_source_operation not in [NOOP, DOES_NOT_EXIST, DELETE] \
        and previous_destination_operation in [NOOP, DOES_NOT_EXIST] \
        and destination_operation in [NOOP, DOES_NOT_EXIST] \
        and source_operation in [NOOP, DOES_NOT_EXIST]:
        return True

    # The DELETE operation in the previous source operation did not replicate to the destination
    if previous_source_operation == DELETE \
    and destination_operation == NOOP:
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

    # Corrupted destination (detected by data change validation) - destination shows changes but source is unchanged
    if source_operation == NOOP \
    and previous_source_operation == NOOP \
    and destination_operation != NOOP \
    and not existing_validation_error:
        return True

    # Row Corrupted (detected by incremental checksums)
    if source_operation == NOOP \
    and not row_verified:
        return True

    # There was previously a validation error, and no changes have happened in source or destination
    if existing_validation_error:
        if source_operation in [NOOP, DOES_NOT_EXIST] and destination_operation in [NOOP, DOES_NOT_EXIST]:
            return True

    return False


if __name__ == "__main__":
    import json
    from colorama import init, Fore, Style

    # Initialize colorama
    init()

    # Load test cases
    with open('validation_logic_tests.json', 'r') as f:
        test_cases = json.load(f)

    # Initialize counters for summary
    total_tests = 0
    passed_tests = 0

    # Test determine_source_operation
    print(f"\n{Style.BRIGHT}Testing determine_source_operation:{Style.RESET_ALL}")
    for test in test_cases['test_determine_source_operation']:
        total_tests += 1
        result = determine_source_operation(test['input']['checksum_1'],
                                         test['input']['checksum_0'])
        expected = Operation(test['expected']['value'])
        passed = result == expected
        if passed:
            passed_tests += 1
            print(f"{test['name']}: {Fore.GREEN}PASS{Style.RESET_ALL}")
        else:
            print(f"{test['name']}: {Fore.RED}FAIL{Style.RESET_ALL}")
            print(f"  Expected: {expected}")
            print(f"  Got: {result}")
            exit(1)

    # Test determine_destination_operation
    print(f"\n{Style.BRIGHT}Testing determine_destination_operation:{Style.RESET_ALL}")
    for test in test_cases['test_determine_destination_operation']:
        total_tests += 1
        result = determine_destination_operation(
            test['input']['destination_present_end'],
            test['input']['destination_updated'],
            test['input']['destination_present_start']
        )
        expected = Operation(test['expected']['value'])
        passed = result == expected
        if passed:
            passed_tests += 1
            print(f"{test['name']}: {Fore.GREEN}PASS{Style.RESET_ALL}")
        else:
            print(f"{test['name']}: {Fore.RED}FAIL{Style.RESET_ALL}")
            print(f"  Expected: {expected}")
            print(f"  Got: {result}")
            exit(1)

    # Test check_for_validation_error
    print(f"\n{Style.BRIGHT}Testing check_for_validation_error:{Style.RESET_ALL}")
    for test in test_cases['test_check_for_validation_error']:
        total_tests += 1
        result = check_for_validation_error(
            Operation(test['input']['source_operation']['value']),
            Operation(test['input']['previous_source_operation']['value']),
            Operation(test['input']['destination_operation']['value']),
            Operation(test['input']['previous_destination_operation']['value']),
            test['input']['existing_validation_error']
        )
        expected = test['expected']
        passed = result == expected
        if passed:
            passed_tests += 1
            print(f"{test['name']}: {Fore.GREEN}PASS{Style.RESET_ALL}")
        else:
            print(f"{test['name']}: {Fore.RED}FAIL{Style.RESET_ALL}")
            print(f"  Expected: {expected}")
            print(f"  Got: {result}")
            exit(1)

    # Test verify_row_integrity_from_incremental_checksums
    print(f"\n{Style.BRIGHT}Testing verify_row_integrity_from_incremental_checksums:{Style.RESET_ALL}")
    for test in test_cases['test_row_integrity_from_incremental_checksums']:
        total_tests += 1
        result = verify_row_integrity_from_incremental_checksums(
            test['input']['incremental_source_checksum'],
            test['input']['incremental_destination_checksum'],
            test['input']['source_checksum'],
            test['input']['destination_checksum'],
        )
        expected = test['expected']
        passed = result == expected
        if passed:
            passed_tests += 1
            print(f"{test['name']}: {Fore.GREEN}PASS{Style.RESET_ALL}")
        else:
            print(f"{test['name']}: {Fore.RED}FAIL{Style.RESET_ALL}")
            print(f"  Expected: {expected}")
            print(f"  Got: {result}")
            exit(1)

    # Test check_for_validation_error_with_row_integrity
    print(f"\n{Style.BRIGHT}Testing check_for_validation_error_with_row_integrity:{Style.RESET_ALL}")
    for test in test_cases['test_check_for_validation_error_with_row_integrity']:
        total_tests += 1
        result = check_for_validation_error(
            Operation(test['input']['source_operation']['value']),
            Operation(test['input']['previous_source_operation']['value']),
            Operation(test['input']['destination_operation']['value']),
            Operation(test['input']['previous_destination_operation']['value']),
            test['input']['existing_validation_error'],
            test['input']['row_verified']
        )
        expected = test['expected']
        passed = result == expected
        if passed:
            passed_tests += 1
            print(f"{test['name']}: {Fore.GREEN}PASS{Style.RESET_ALL}")
        else:
            print(f"{test['name']}: {Fore.RED}FAIL{Style.RESET_ALL}")
            print(f"  Expected: {expected}")
            print(f"  Got: {result}")
            exit(1)

    # Print summary
    print(f"\n{Style.BRIGHT}Test Summary:{Style.RESET_ALL}")
    print(f"Total tests: {total_tests}")
    print(f"Passed: {Fore.GREEN}{passed_tests}{Style.RESET_ALL}")
    if passed_tests == total_tests:
        print(f"{Fore.GREEN}All tests passed!{Style.RESET_ALL}")
    else:
        print(f"{Fore.RED}Failed: {total_tests - passed_tests}{Style.RESET_ALL}")
