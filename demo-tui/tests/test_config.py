#!/usr/bin/env python3
"""
Test Configuration Module for Seatbelt Demo

This module provides functionality to run tests using the configuration file format.
"""

import os
import sys
import yaml
import argparse
import logging
from typing import Dict, List, Any, Optional
from seatbelt_demo.simulator import Simulator
from seatbelt_demo.simulator.database import SchemaDefinition, ColumnDefinition, ColumnType, InitialData
from seatbelt_demo.simulator.schema_utils import convert_schema_dict, convert_initial_data_dict

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)

# Helper function to detect if terminal supports colors
def supports_color():
    """
    Returns True if the running system's terminal supports color,
    and False otherwise.
    """
    # Check if the NO_COLOR environment variable is set
    if os.environ.get('NO_COLOR', False):
        return False
        
    # Check if output is a tty
    if not hasattr(sys.stdout, 'isatty') or not sys.stdout.isatty():
        return False
        
    # Check platform
    plat = sys.platform
    supported_platform = plat != 'Pocket PC' and (plat != 'win32' or 'ANSICON' in os.environ)
    
    # On Windows, check if TERM environment variable is set
    if plat == 'win32' and not supported_platform:
        return os.environ.get('TERM', '') == 'ANSI'
        
    return supported_platform

class TestResult:
    """Class representing a test result"""
    
    def __init__(self, name: str, passed: bool, message: Optional[str] = None):
        self.name = name
        self.passed = passed
        self.message = message

    def __str__(self):
        result = "PASS" if self.passed else "FAIL"
        output = f"{self.name}: {result}"
        if self.message and not self.passed:
            output += f" - {self.message}"
        return output

    def colored_str(self):
        """Return colored string representation of the test result"""
        if self.passed:
            result = "\033[92mPASS\033[0m"  # Green
        else:
            result = "\033[91mFAIL\033[0m"  # Red
            
        output = f"{self.name}: {result}"
        if self.message and not self.passed:
            output += f" - {self.message}"
        return output


class TestRunner:
    """Class for running tests based on configuration files"""
    
    def __init__(self, verbose: bool = False, use_color: bool = True):
        self.verbose = verbose
        self.use_color = use_color
        self.results = []
    
    def run_test_from_file(self, test_file: str) -> List[TestResult]:
        """Run a test from a YAML configuration file"""
        try:
            with open(test_file, 'r') as f:
                config = yaml.safe_load(f)
            
            test_name = os.path.basename(test_file)
            return self.run_test(config, test_name)
            
        except Exception as e:
            return [TestResult(test_file, False, f"Error loading test: {str(e)}")]
    
    def run_test(self, config: Dict[str, Any], test_name: str) -> List[TestResult]:
        """Run a test based on a configuration dict"""
        results = []
        
        # Initialize simulator with config settings
        schema_dict = config.get('schema')
        schema = None
        if schema_dict:
            schema = convert_schema_dict(schema_dict)
            
        initial_data_dict = config.get('initial_data')
        initial_data = None
        if initial_data_dict:
            initial_data = convert_initial_data_dict(initial_data_dict)
            
        simulator = Simulator(
            random_seed=config.get('random_seed', 42), 
            schema=schema,
            initial_data=initial_data
        )
        
        # Run the plan
        plan = config.get('plan', [])
        if self.verbose:
            logging.info(f"Executing plan with {len(plan)} steps")
        
        # Execute each step in the plan
        for step in plan:
            operation = step.get('operation')
            if not operation:
                results.append(TestResult(
                    f"{test_name} - Invalid step", 
                    False, 
                    "Missing operation field in plan step"
                ))
                continue
            
            # Check expectations inline
            if operation == 'expect':
                result = self._check_expectation(simulator, step, test_name)
                results.append(result)
                continue # Move to the next step after checking expectation
                
            # Execute other operations
            try:
                self._execute_operation(simulator, step)
            except Exception as e:
                results.append(TestResult(
                    f"{test_name} - Error executing {operation}", 
                    False, 
                    str(e)
                ))
                break # Stop plan execution on operational error
        
        # Process top-level expectations after running plan
        top_level_expectations = self._get_top_level_expectations(config)
        for exp in top_level_expectations:
            result = self._check_expectation(simulator, exp, test_name)
            results.append(result)
            
        # Optional: Check for unexpected validation errors if no expectations were specified *at all*
        all_expectations = self._get_all_expectations(config) # Helper needed to get both top-level and plan
        if not all_expectations and 'seatbelt_check' in [s.get('operation') for s in plan]:
            final_metrics = simulator.metrics_tracker.get()
            if final_metrics['error_count'] > 0:
                results.append(TestResult(
                    f"{test_name} - Unexpected validation errors", 
                    False, 
                    f"Found {final_metrics['error_count']} validation errors"
                ))
        
        return results
    
    def _get_top_level_expectations(self, config: Dict[str, Any]) -> List[Dict[str, Any]]:
        """Extract only top-level expectations from the config"""
        expectations = []
        if 'expect' in config:
            expectations.extend(config['expect'] if isinstance(config['expect'], list) else [config['expect']])
        return expectations
    
    def _get_all_expectations(self, config: Dict[str, Any]) -> List[Dict[str, Any]]:
        """Extract all expectations from the config (top-level and plan)"""
        expectations = self._get_top_level_expectations(config)
        for step in config.get('plan', []):
            if step.get('operation') == 'expect':
                expectations.append(step)
        return expectations
    
    def _execute_operation(self, simulator: Simulator, step: Dict[str, Any]) -> None:
        """Execute a single operation step"""
        operation = step.get('operation')
        
        # Handle the new initialize operation (sequence of operations)
        if operation == 'initialize':
            simulator.seatbelt_check()
            simulator.extract()
            simulator.load()
            simulator.seatbelt_check()
            return
            
        # Handle basic simulator operations
        if operation == 'insert':
            row = step.get('row')
            simulator.insert_row(row)
        elif operation == 'insert_with_null':
            simulator.insert_with_null()
        elif operation == 'update':
            row = step.get('row', {})
            # First check if row_id is specified in the row data
            row_id = row.get('id') if row else None
            # If not, check if it's specified at the top level
            if row_id is None:
                row_id = step.get('id')
            simulator.update_row(row_id, row)
        elif operation == 'update_with_null':
            simulator.update_with_null()
        elif operation == 'delete':
            simulator.delete_row()
        elif operation == 'extract':
            simulator.extract()
        elif operation == 'load':
            simulator.load()
        elif operation == 'seatbelt_check':
            simulator.seatbelt_check()
        elif operation == 'corrupt_by_update':
            # Check if a specific row is provided for corruption
            if 'row' in step:
                row_data = step['row'].copy()
                simulator.corrupt_by_update(row_data)
            else:
                simulator.corrupt_by_update()
        elif operation == 'corrupt_by_insert':
            simulator.corrupt_by_insert()
        elif operation == 'corrupt_by_delete':
            simulator.corrupt_by_delete()
        elif operation == 'corrupt_target':
            # Check if a specific row is provided for corruption
            if 'row' in step:
                row_data = step['row'].copy()
                # Extract row_id if specified in the row data
                row_id = row_data.get('id')
                if row_id is not None:
                    simulator.corrupt_target_with_row(row_id, row_data)
                else:
                    simulator.corrupt_target_score()
            else:
                # Fall back to random corruption
                simulator.corrupt_target_score()
        elif operation == 'toggle_null_corruption':
            simulator.toggle_null_corruption()
        elif operation == 'random_operation':
            simulator.random_operation()
        else:
            raise ValueError(f"Unknown operation: {operation}")
    
    def _check_expectation(self, simulator: Simulator, expectation: Dict[str, Any], test_name: str) -> TestResult:
        """Check if an expectation is satisfied"""
        expect_type = expectation.get('type', 'metric')
        name = expectation.get('name', 'Unknown expectation')
        
        full_name = f"{test_name} - {name}"
        
        if expect_type == 'metric':
            metric_name = expectation.get('metric')
            if not metric_name:
                return TestResult(full_name, False, "Missing metric name in expectation")
                
            expected_value = expectation.get('value')
            if expected_value is None:
                return TestResult(full_name, False, "Missing expected value in expectation")
                
            actual_value = simulator.metrics_tracker.get(metric_name)
            comparison = expectation.get('comparison', 'equal')
            
            if comparison == 'equal':
                passed = actual_value == expected_value
            elif comparison == 'not_equal':
                passed = actual_value != expected_value
            elif comparison == 'greater_than':
                passed = actual_value > expected_value
            elif comparison == 'less_than':
                passed = actual_value < expected_value
            elif comparison == 'greater_or_equal':
                passed = actual_value >= expected_value
            elif comparison == 'less_or_equal':
                passed = actual_value <= expected_value
            else:
                return TestResult(full_name, False, f"Unknown comparison type: {comparison}")
                
            message = f"Expected {metric_name} to be {comparison} {expected_value}, got {actual_value}"
            return TestResult(full_name, passed, None if passed else message)
            
        elif expect_type == 'validation_error':
            expected = expectation.get('expected', True)
            actual = simulator.metrics_tracker.get('error_count') > 0
            passed = actual == expected
            message = f"Expected validation {'errors' if expected else 'success'}, got {'errors' if actual else 'success'}"
            return TestResult(full_name, passed, None if passed else message)
            
        elif expect_type == 'row_exists':
            row_id = expectation.get('id')
            if row_id is None:
                return TestResult(full_name, False, "Missing row ID in expectation")
                
            target = expectation.get('target', 'source')
            if target == 'source':
                exists = row_id in simulator.database.source_db
            elif target == 'target':
                exists = row_id in simulator.database.target_db
            else:
                return TestResult(full_name, False, f"Unknown target: {target}")
                
            expected = expectation.get('exists', True)
            passed = exists == expected
            message = f"Expected row {row_id} to {'exist' if expected else 'not exist'} in {target}, but it does {'exist' if exists else 'not exist'}"
            return TestResult(full_name, passed, None if passed else message)
            
        else:
            return TestResult(full_name, False, f"Unknown expectation type: {expect_type}")
    
    def print_results(self):
        """Print test results to console with colors and summary at bottom"""
        pass_count = sum(1 for r in self.results if r.passed)
        fail_count = len(self.results) - pass_count
        
        # Print individual test results first
        if fail_count > 0:
            print("\nFailed tests:")
            for result in self.results:
                if not result.passed:
                    if self.use_color:
                        print(f"  {result.colored_str()}")
                    else:
                        print(f"  {result}")
                
        if self.verbose or fail_count == 0:
            print("\nAll tests:")
            for result in self.results:
                if self.use_color:
                    print(f"  {result.colored_str()}")
                else:
                    print(f"  {result}")
        
        # Print summary at the bottom
        print("\n" + "=" * 50)
        if fail_count == 0:
            if self.use_color:
                print(f"\033[92mAll tests passed!\033[0m ({pass_count} tests)")
            else:
                print(f"All tests passed! ({pass_count} tests)")
        else:
            if self.use_color:
                print(f"Test Results: \033[92m{pass_count} passed\033[0m, \033[91m{fail_count} failed\033[0m")
            else:
                print(f"Test Results: {pass_count} passed, {fail_count} failed")
        print("=" * 50)


def run_tests(test_paths, verbose=False, use_color=None):
    """Run tests from the specified files or directories"""
    # Auto-detect color support if not explicitly specified
    if use_color is None:
        use_color = supports_color()
        
    runner = TestRunner(verbose=verbose, use_color=use_color)
    runner.results = []
    
    # Process each path
    for path in test_paths:
        if os.path.isdir(path):
            # If it's a directory, run all YAML files within it
            for root, dirs, files in os.walk(path):
                for file in files:
                    if file.endswith(('.yaml', '.yml')):
                        file_path = os.path.join(root, file)
                        if verbose:
                            print(f"Running test file: {file_path}")
                        results = runner.run_test_from_file(file_path)
                        runner.results.extend(results)
        elif os.path.isfile(path):
            # If it's a file, run it directly
            if verbose:
                print(f"Running test file: {path}")
            results = runner.run_test_from_file(path)
            runner.results.extend(results)
        else:
            print(f"Error: Path not found: {path}")
    
    # Print results
    runner.print_results()
    
    # Return True if all tests passed, False otherwise
    return all(result.passed for result in runner.results)


def main():
    parser = argparse.ArgumentParser(description='Run tests using Seatbelt Demo config files')
    parser.add_argument('test_paths', nargs='+', help='Paths to test files or directories')
    parser.add_argument('-v', '--verbose', action='store_true', help='Enable verbose output')
    
    args = parser.parse_args()
    
    success = run_tests(args.test_paths, args.verbose)
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main() 