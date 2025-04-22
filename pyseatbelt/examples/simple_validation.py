#!/usr/bin/env python3
"""
Simple example demonstrating the use of pyseatbelt ValidationEngine.

This example creates custom Source and Target implementations
and runs validation between them.
"""

import logging
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..'))

# Import from pyseatbelt
from pyseatbelt.validation import Source, Target, ValidationEngine


class DictSource(Source):
    """A simple Source implementation that uses a dictionary as data store."""
    
    def __init__(self, data):
        """Initialize with dictionary data.
        
        Args:
            data: Dictionary of ID to row data
        """
        self.data = data
        self.changes = {}
        
    def read_change_log_changes(self, column_names):
        """Read changes from the source.
        
        In this simple implementation, we don't track changes,
        so we return an empty dictionary.
        """
        return {}
        
    def read_signatures(self, column_names):
        """Read signatures from the source."""
        return {id: hash(str(row)) for id, row in self.data.items()}


class DictTarget(Target):
    """A simple Target implementation that uses a dictionary as data store."""
    
    def __init__(self, data):
        """Initialize with dictionary data.
        
        Args:
            data: Dictionary of ID to row data
        """
        self.data = data
        
    def read_signatures(self, column_names):
        """Read signatures from the target."""
        return {id: hash(str(row)) for id, row in self.data.items()}


def main():
    """Run a simple validation example."""
    # Configure logging
    logging.basicConfig(level=logging.INFO, 
                        format='%(asctime)s - %(levelname)s - %(message)s')
    
    # Create source and target data
    source_data = {
        1: {'name': 'Alice', 'age': 30},
        2: {'name': 'Bob', 'age': 25},
        3: {'name': 'Charlie', 'age': 35},
        4: {'name': 'David', 'age': 40},
    }
    
    target_data = {
        1: {'name': 'Alice', 'age': 30},
        2: {'name': 'Bob', 'age': 25},
        3: {'name': 'Charlie', 'age': 38},  # Different age
        # ID 4 is missing
        5: {'name': 'Eve', 'age': 45},  # Extra row
    }
    
    # Create source and target
    source = DictSource(source_data)
    target = DictTarget(target_data)
    
    # Create validation engine
    engine = ValidationEngine()
    
    # Run validation
    print("Running validation...")
    metrics = engine.seatbelt_check(source, target)
    
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
            print(f"  Source signature: {entry['source_signature']}")
            print(f"  Target signature: {entry['target_signature']}")
            print(f"  Source operation: {entry['source_operation']}")
            print(f"  Target operation: {entry['target_operation']}")


if __name__ == "__main__":
    main() 