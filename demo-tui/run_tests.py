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
    parser.add_argument('--no-color', action='store_true',
                        help='Disable color output')
    parser.add_argument('--force-color', action='store_true',
                        help='Force color output even when stdout is not a TTY')
    parser.add_argument('--print-db', action='store_true',
                        help='Print the source and target databases at the end of each test')
    args = parser.parse_args()
    
    # Determine color setting
    use_color = None  # None means auto-detect
    if args.no_color:
        use_color = False
    elif args.force_color:
        use_color = True
    
    # Run tests
    success = run_tests(args.paths, args.verbose, use_color=use_color, print_db=args.print_db)
    sys.exit(0 if success else 1) 