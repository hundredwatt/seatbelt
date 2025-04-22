"""Tests for the pyseatbelt validation engine."""

import unittest
from typing import List, Dict, Any, Tuple

# Add reference directory to path for imports
import sys
import os
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', '..', 'reference'))

# Import from our package
from pyseatbelt.validation import Source, Target, ValidationEngine


class TestSource(Source):
    """Test implementation of Source for unit testing."""

    def __init__(self, source_data=None, changes=None):
        """Initialize the test source.

        Args:
            source_data: Optional dictionary of ID to row data
            changes: Optional dictionary of ID to change data
        """
        self.source_db = source_data or [
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
            {'id': 3, 'name': 'Jim', 'age': 35},
        ]
        self.changes = changes or {}

    def read_change_log_changes(self, column_names: List[str]) -> Dict[Any, Tuple[Any, Any]]:
        """Read changes from the test source.

        Args:
            column_names: List of column names to include (ignored in test)

        Returns:
            Dictionary of ID to (source_signature, target_signature)
        """
        # For test purposes, return changes for rows with ID > 2
        return {
            row['id']: (
                hash((row['name'], row['age'])),  # Source signature
                hash((row['name'], row['age']))  # Computed target signature from source data
            )
            for row in self.source_db if row['id'] > 2
        }

    def read_signatures(self, column_names: List[str]) -> Dict[Any, Any]:
        """Read signatures from the test source.

        Args:
            column_names: List of column names to include (ignored in test)

        Returns:
            Dictionary of ID to signature
        """
        return {row['id']: hash((row['name'], row['age'])) for row in self.source_db}


class TestTarget(Target):
    """Test implementation of Target for unit testing."""

    def __init__(self, target_data=None):
        """Initialize the test target.

        Args:
            target_data: Optional dictionary of ID to row data
        """
        self.target_db = target_data or [
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
            {'id': 3, 'name': 'Jim', 'age': 35},
        ]

    def read_signatures(self, column_names: List[str]) -> Dict[Any, Any]:
        """Read signatures from the test target.

        Args:
            column_names: List of column names to include (ignored in test)

        Returns:
            Dictionary of ID to signature
        """
        return {row['id']: hash((row['name'], row['age'])) for row in self.target_db}


class TestValidationEngine(unittest.TestCase):
    """Test cases for ValidationEngine."""

    def setUp(self):
        """Set up the test case."""
        self.engine = ValidationEngine()

    def test_basic_validation(self):
        """Test basic validation between source and target."""
        source = TestSource()
        target = TestTarget()

        # Corrupt target
        target.target_db = target.target_db.copy()
        target.target_db[2]['age'] = 1337

        # Run validation
        metrics = self.engine.seatbelt_check(source, target)

        # Check metrics
        self.assertEqual(metrics['source_size'], 3)
        self.assertEqual(metrics['target_size'], 3)
        self.assertEqual(metrics['seatbelt_size'], 3)

        # We expect one error due to the age difference for ID 3
        self.assertEqual(metrics['pending_count'], 1)

    def test_missing_row_in_target(self):
        """Test validation when a row is missing in the target."""
        source = TestSource()
        target = TestTarget([
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
            # ID 3 is missing from target
        ])

        # Run validation
        metrics = self.engine.seatbelt_check(source, target)

        # Check metrics
        self.assertEqual(metrics['source_size'], 3)
        self.assertEqual(metrics['target_size'], 2)
        self.assertEqual(metrics['pending_count'], 1)  # Error due to missing row

    def test_extra_row_in_target(self):
        """Test validation when there's an extra row in the target."""
        source = TestSource([
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
        ])
        target = TestTarget([
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
            {'id': 3, 'name': 'Extra', 'age': 40},  # Extra row in target
        ])

        # Run validation
        metrics = self.engine.seatbelt_check(source, target)

        # Check metrics
        self.assertEqual(metrics['source_size'], 2)
        self.assertEqual(metrics['target_size'], 3)
        self.assertEqual(metrics['pending_count'], 1)  # Error due to extra row

    def test_pending_changes(self):
        """Test validation with pending changes."""
        # Updated source has a new row
        source = TestSource([
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
            {'id': 3, 'name': 'Jim', 'age': 35},
            {'id': 4, 'name': 'NewRow', 'age': 50},  # New row
        ])
        target = TestTarget([
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
            {'id': 3, 'name': 'Jim', 'age': 35},
            # ID 4 is not yet in target
        ])

        # Run validation
        metrics = self.engine.seatbelt_check(source, target)

        # Check metrics - expect pending change for ID 4
        self.assertEqual(metrics['source_size'], 4)
        self.assertEqual(metrics['target_size'], 3)
        self.assertEqual(metrics['pending_count'], 1)


if __name__ == '__main__':
    unittest.main()
