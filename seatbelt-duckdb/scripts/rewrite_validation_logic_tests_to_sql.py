import os
import json

reference_dir = os.path.join(os.path.dirname(__file__), '..', '..', 'reference')
test_dir = os.path.join(os.path.dirname(__file__), '..', 'test', 'sql')

validation_logic_tests_file = os.path.join(reference_dir, 'validation_logic_tests.json')
sql_test_output_file = os.path.join(test_dir, 'validation_logic.test')

# Load the test cases from JSON
with open(validation_logic_tests_file, 'r') as f:
    test_cases = json.load(f)

# Create the SQL output file content
sql_output = [
    "# name: test/sql/validation_logic.test",
    "# description: test validation logic functions in seatbelt_duckdb extension",
    "# group: [seatbelt_duckdb]",
    "",
    "# Require statement will ensure this test is run with this extension loaded",
    "require seatbelt_duckdb",
    ""
]

# Process determine_source_operation tests
sql_output.append("# Testing determine_source_operation_varchar function")
for i, test in enumerate(test_cases['test_determine_source_operation']):
    name = test['name']
    checksum_1 = "NULL" if test['input']['checksum_1'] is None else f"'{test['input']['checksum_1']}'"
    checksum_0 = "NULL" if test['input']['checksum_0'] is None else f"'{test['input']['checksum_0']}'"
    expected = test['expected']['value']
    
    sql_output.extend([
        f"# Test test_determine_source_operation[{i}]: {name}",
        "query I",
        f"SELECT determine_source_operation_varchar({checksum_1}, {checksum_0});",
        "----",
        f"{expected}"
    ])
    sql_output.append("")

# Process determine_destination_operation tests
sql_output.append("# Testing determine_destination_operation function")
for i, test in enumerate(test_cases['test_determine_destination_operation']):
    name = test['name']
    dest_present_end = str(test['input']['destination_present_end']).lower()
    dest_updated = str(test['input']['destination_updated']).lower()
    dest_present_start = str(test['input']['destination_present_start']).lower()
    expected = test['expected']['value']
    
    sql_output.extend([
        f"# Test test_determine_destination_operation[{i}]: {name}",
        "query I",
        f"SELECT determine_destination_operation({dest_present_end}, {dest_updated}, {dest_present_start});",
        "----",
        f"{expected}"
    ])
    sql_output.append("")

# Process verify_row_integrity_from_incremental_checksums tests
sql_output.append("# Testing verify_row_integrity_varchar function")
for i, test in enumerate(test_cases['test_row_integrity_from_incremental_checksums']):
    name = test['name']
    inc_source = "NULL" if test['input']['incremental_source_checksum'] is None else f"'{test['input']['incremental_source_checksum']}'"
    inc_dest = "NULL" if test['input']['incremental_destination_checksum'] is None else f"'{test['input']['incremental_destination_checksum']}'"
    source = "NULL" if test['input']['source_checksum'] is None else f"'{test['input']['source_checksum']}'"
    dest = "NULL" if test['input']['destination_checksum'] is None else f"'{test['input']['destination_checksum']}'"
    expected = str(test['expected']).lower()
    
    sql_output.extend([
        f"# Test test_row_integrity_from_incremental_checksums[{i}]: {name}",
        "query T",
        f"SELECT verify_row_integrity_varchar({inc_source}, {inc_dest}, {source}, {dest});",
        "----",
        f"{expected}"
    ])
    sql_output.append("")

# Process check_for_validation_error (base version) tests
sql_output.append("# Testing check_for_validation_error_base function")
for i, test in enumerate(test_cases['test_check_for_validation_error']):
    name = test['name']
    source_op = test['input']['source_operation']['value']
    prev_source_op = test['input']['previous_source_operation']['value']
    dest_op = test['input']['destination_operation']['value']
    prev_dest_op = test['input']['previous_destination_operation']['value']
    existing_error = str(test['input']['existing_validation_error']).lower()
    expected = str(test['expected']).lower()
    
    sql_output.extend([
        f"# Test test_check_for_validation_error[{i}]: {name}",
        "query T",
        f"SELECT check_for_validation_error_base({source_op}, {prev_source_op}, {dest_op}, {prev_dest_op}, {existing_error});",
        "----",
        f"{expected}"
    ])
    sql_output.append("")

# Process check_for_validation_error_with_row_integrity tests
sql_output.append("# Testing check_for_validation_error_with_row_integrity function")
for i, test in enumerate(test_cases['test_check_for_validation_error_with_row_integrity']):
    name = test['name']
    source_op = test['input']['source_operation']['value']
    prev_source_op = test['input']['previous_source_operation']['value']
    dest_op = test['input']['destination_operation']['value']
    prev_dest_op = test['input']['previous_destination_operation']['value']
    existing_error = str(test['input']['existing_validation_error']).lower()
    row_verified = str(test['input']['row_verified']).lower()
    expected = str(test['expected']).lower()
    
    sql_output.extend([
        f"# Test test_check_for_validation_error_with_row_integrity[{i}]: {name}",
        "query T",
        f"SELECT check_for_validation_error_with_row_integrity({source_op}, {prev_source_op}, {dest_op}, {prev_dest_op}, {existing_error}, {row_verified});",
        "----",
        f"{expected}"
    ])
    sql_output.append("")

# Write the SQL output file
with open(sql_test_output_file, 'w') as f:
    f.write('\n'.join(sql_output))

print(f"SQL test file generated: {sql_test_output_file}")
