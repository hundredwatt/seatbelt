#!/usr/bin/env python3
"""
Main entry point for running pyseatbelt directly.
"""

import sys
import os
import logging

# Import validation logic dynamically from reference directory
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', 'reference'))

from pyseatbelt.validation import ValidationEngine
from tests.pyseatbelt.test_validation import TestSource, TestTarget

def main():
    """Run a demo with test source and target."""
    # Configure logging
    logging.basicConfig(
        level=logging.INFO,
        format='%(asctime)s - %(levelname)s - %(message)s'
    )

    # Create validation engine
    engine = ValidationEngine()

    # Create test data sources
    source = TestSource()
    target = TestTarget()

    # Corrupt target
    target.target_db[2]['age'] = 1337

    # Run validation
    print("Running validation...")
    metrics = engine.seatbelt_check(source, target)
    # 2nd run - so pending -> error
    metrics = engine.seatbelt_check(source, target)

    assert(metrics['valid_count'] == 2)
    assert(metrics['error_count'] == 1)

    # Print results
    print("\nValidation Results:")
    print(f"Source size: {metrics['source_size']}")
    print(f"Target size: {metrics['target_size']}")
    print(f"Valid rows: {metrics['valid_count']}")
    print(f"Pending rows: {metrics['pending_count']}")
    print(f"Error rows: {metrics['error_count']}")

    # Print specific errors
    for id, entry in engine.seatbelt.items():
        if entry.get('validation_error'):
            print(f"\nError for ID {id}:")
            print(f"  Source signature:         {entry['source_signature']}")
            print(f"  Inc. source signature:    {entry['incremental_source_signature']}")
            print(f"  Target signature:         {entry['target_signature']}")
            print(f"  Inc. target signature:    {entry['incremental_target_signature']}")
            print(f"  Source operation:         {entry['source_operation']}")
            print(f"  Target operation:         {entry['target_operation']}")

    return 0

if __name__ == "__main__":
    sys.exit(main())
