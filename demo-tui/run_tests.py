#!/usr/bin/env python3
"""
Seatbelt Demo Test Runner

A simple script to run tests using the test configuration framework.
"""

import sys
import argparse
from tests.test_config import run_tests

if __name__ == "__main__":
    # Parse command-line arguments
    parser = argparse.ArgumentParser(description='Run Seatbelt Demo tests')
    parser.add_argument('paths', nargs='*', default=['tests'], 
                        help='Paths to test files or directories')
    parser.add_argument('-v', '--verbose', action='store_true',
                        help='Enable verbose output')
    args = parser.parse_args()
    
    # Run tests
    success = run_tests(args.paths, args.verbose)
    sys.exit(0 if success else 1) 