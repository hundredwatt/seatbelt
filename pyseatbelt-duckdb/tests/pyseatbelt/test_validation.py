"""Tests for the pyseatbelt validation engine."""

import unittest
import tempfile
import os
import logging
from typing import List, Dict, Any, Tuple

# Add reference directory to path for imports
import sys
import os

# Import from our package
from pyseatbelt.validation import Source, Target, ValidationEngine

# Set up logging for this test module
logger = logging.getLogger(__name__)

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
    
    def test_deleted_row(self):
        """Test validation when a row is deleted in the source."""
        source = TestSource([
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
        ])
        target = TestTarget([
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
            {'id': 3, 'name': 'Jim', 'age': 35},
        ])

        # Run validation
        metrics = self.engine.seatbelt_check(source, target)

        # Check metrics - expect pending change for ID 4
        self.assertEqual(metrics['source_size'], 2)
        self.assertEqual(metrics['target_size'], 3)
        self.assertEqual(metrics['pending_count'], 1)
        self.assertEqual(metrics['error_count'], 0)

        target.target_db = target.target_db.copy()
        target.target_db.pop(2)

        # Run validation
        metrics2 = self.engine.seatbelt_check(source, target)

        # Check metrics - expect pending change for ID 4
        self.assertEqual(metrics2['source_size'], 2)
        self.assertEqual(metrics2['target_size'], 2)
        self.assertEqual(metrics2['pending_count'], 0)
        self.assertEqual(metrics2['error_count'], 0)

        # Gone entry removed from shadow
        self.assertNotIn(3, self.engine.shadow.sql("SELECT pk FROM shadow").df()['pk'].tolist())

    def test_save_load_shadow(self):
        """Test saving and loading the shadow file, verifying pending-to-error transition."""
        # Create a temporary file for testing
        with tempfile.NamedTemporaryFile(delete=False) as temp_file:
            shadow_path = temp_file.name
            os.remove(shadow_path)
        
        try:
            # Setup: initial state with a new row that should be pending
            initial_source = TestSource([
                {'id': 1, 'name': 'John', 'age': 30},
                {'id': 2, 'name': 'Jane', 'age': 25},
                {'id': 3, 'name': 'Jim', 'age': 35},
                {'id': 4, 'name': 'NewRow', 'age': 50},  # New row not in target
            ])
            initial_target = TestTarget([
                {'id': 1, 'name': 'John', 'age': 30},
                {'id': 2, 'name': 'Jane', 'age': 25},
                {'id': 3, 'name': 'Jim', 'age': 35},
                # ID 4 is not yet in target - this should be PENDING
            ])
            
            # First run - should mark ID 4 as pending
            engine1 = ValidationEngine()
            metrics1 = engine1.seatbelt_check(initial_source, initial_target)
            
            # Verify ID 4 is pending
            self.assertEqual(metrics1['pending_count'], 1)
            self.assertEqual(metrics1['error_count'], 0)
            
            # Save the shadow state
            engine1.save_shadow(shadow_path)
            
            # Now for the second run, simulate a long time passing
            # Source still has ID 4, but we expect it to be an error now since
            # it should have already propagated to the target
            
            # Load shadow from file
            engine2 = ValidationEngine(shadow_file=shadow_path)
            
            # Use same source and target
            metrics2 = engine2.seatbelt_check(initial_source, initial_target)
            
            # Now ID 4 should be an error because it's been in the shadow for "too long"
            # and still hasn't appeared in the target
            self.assertEqual(metrics2['error_count'], 1)
            self.assertEqual(metrics2['pending_count'], 0)
            
            # Verify that shadow file was actually used by checking shadow data
            # ID 4 should have these operations in the shadow
            shadow_data = engine2.fetchall_shadow()[4]
            self.assertIsNotNone(shadow_data)
            self.assertEqual(shadow_data['validation_error'], True)
            
        finally:
            # Clean up the temporary file
            if os.path.exists(shadow_path):
                os.unlink(shadow_path)

    def test_partitioning(self):
        """Test validation with partitioning enabled."""
        # Create a source with IDs that will end up in different partitions
        source = TestSource([
            {'id': 1, 'name': 'John', 'age': 30},   # Mod 3 = 1
            {'id': 2, 'name': 'Jane', 'age': 25},   # Mod 3 = 2
            {'id': 3, 'name': 'Jim', 'age': 35},    # Mod 3 = 0
            {'id': 4, 'name': 'Janet', 'age': 40},  # Mod 3 = 1
            {'id': 5, 'name': 'Jack', 'age': 45},   # Mod 3 = 2
            {'id': 6, 'name': 'Jill', 'age': 50},   # Mod 3 = 0
        ])
        
        # Create a matching target
        target = TestTarget([
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
            {'id': 3, 'name': 'Jim', 'age': 35},
            {'id': 4, 'name': 'Janet', 'age': 40},
            {'id': 5, 'name': 'Jack', 'age': 45},
            {'id': 6, 'name': 'Jill', 'age': 50},
        ])
        
        # Introduce an error for ID 5 (which is in partition 2)
        target.target_db[4]['age'] = 46
        
        # Run validation with partition 0 (should include IDs 3 and 6)
        metrics_p0 = self.engine.seatbelt_check(source, target, partitions=3, current_partition=0)
        
        # Check that only partition 0 rows are in the shadow
        shadow_data = self.engine.fetchall_shadow() 
        self.assertEqual(len(shadow_data), 2)
        self.assertIn(3, shadow_data)
        self.assertIn(6, shadow_data)
        self.assertEqual(metrics_p0['seatbelt_size'], 2)
        self.assertEqual(metrics_p0['valid_count'], 2)
        
        # Reset engine and run with partition 1 (should include IDs 1 and 4)
        self.engine = ValidationEngine()
        metrics_p1 = self.engine.seatbelt_check(source, target, partitions=3, current_partition=1)
        
        # Check that only partition 1 rows are in the shadow
        shadow_data = self.engine.fetchall_shadow() 
        self.assertEqual(len(shadow_data), 2)
        self.assertIn(1, shadow_data)
        self.assertIn(4, shadow_data)
        self.assertEqual(metrics_p1['seatbelt_size'], 2)
        self.assertEqual(metrics_p1['valid_count'], 2)
        
        # Reset engine and run with partition 2 (should include IDs 2 and 5)
        self.engine = ValidationEngine()
        metrics_p2 = self.engine.seatbelt_check(source, target, partitions=3, current_partition=2)
        
        # Check that only partition 2 rows are in the shadow and ID 5 has an error
        shadow_data = self.engine.fetchall_shadow() 
        self.assertEqual(len(shadow_data), 2)
        self.assertIn(2, shadow_data)
        self.assertIn(5, shadow_data)
        self.assertEqual(metrics_p2['seatbelt_size'], 2)
        self.assertEqual(metrics_p2['valid_count'], 1)  # Only ID 2 should be valid
        self.assertEqual(metrics_p2['pending_count'], 1)  # ID 5 should be pending due to age difference

    def test_id_range_filtering(self):
        """Test validation with ID range filtering."""
        # Create a source with a range of IDs
        source = TestSource([
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
            {'id': 3, 'name': 'Jim', 'age': 35},
            {'id': 4, 'name': 'Janet', 'age': 40},
            {'id': 5, 'name': 'Jack', 'age': 45},
            {'id': 6, 'name': 'Jill', 'age': 50},
        ])
        
        # Create a matching target with an error for ID 5
        target = TestTarget([
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
            {'id': 3, 'name': 'Jim', 'age': 35},
            {'id': 4, 'name': 'Janet', 'age': 40},
            {'id': 5, 'name': 'Jack', 'age': 46},  # Error in age
            {'id': 6, 'name': 'Jill', 'age': 50},
        ])
        
        # Test with minimum range only (ID >= 3)
        self.engine = ValidationEngine()
        metrics_min = self.engine.seatbelt_check(source, target, id_range=(3, None))
        
        # Check that only IDs >= 3 are in the shadow
        shadow_data = self.engine.fetchall_shadow() 
        self.assertEqual(len(shadow_data), 4)
        for id in [3, 4, 5, 6]:
            self.assertIn(id, shadow_data)
        for id in [1, 2]:
            self.assertNotIn(id, shadow_data)
        self.assertEqual(metrics_min['seatbelt_size'], 4)
        self.assertEqual(metrics_min['pending_count'], 1)  # ID 5 has an error
        
        # Test with maximum range only (ID < 5)
        self.engine = ValidationEngine()
        metrics_max = self.engine.seatbelt_check(source, target, id_range=(None, 5))
        
        # Check that only IDs < 5 are in the shadow
        shadow_data = self.engine.fetchall_shadow() 
        self.assertEqual(len(shadow_data), 4)
        for id in [1, 2, 3, 4]:
            self.assertIn(id, shadow_data)
        for id in [5, 6]:
            self.assertNotIn(id, shadow_data)
        self.assertEqual(metrics_max['seatbelt_size'], 4)
        self.assertEqual(metrics_max['valid_count'], 4)  # All rows should be valid
        
        # Test with both min and max (3 <= ID < 6)
        self.engine = ValidationEngine()
        metrics_range = self.engine.seatbelt_check(source, target, id_range=(3, 6))
        
        # Check that only IDs in range are in the shadow
        shadow_data = self.engine.fetchall_shadow() 
        self.assertEqual(len(shadow_data), 3)
        for id in [3, 4, 5]:
            self.assertIn(id, shadow_data)
        for id in [1, 2, 6]:
            self.assertNotIn(id, shadow_data)
        self.assertEqual(metrics_range['seatbelt_size'], 3)
        self.assertEqual(metrics_range['pending_count'], 1)  # ID 5 has an error

    def test_combined_criteria_precedence(self):
        """Test that ID range filtering is properly applied at the shadow level."""
        # Create a source with various IDs for testing range filtering
        source = TestSource([
            {'id': 1, 'name': 'John', 'age': 30},
            {'id': 2, 'name': 'Jane', 'age': 25},
            {'id': 3, 'name': 'Jim', 'age': 35},
            {'id': 10, 'name': 'Janet', 'age': 40},
            {'id': 11, 'name': 'Jack', 'age': 45},
            {'id': 12, 'name': 'Jill', 'age': 50},
        ])
        
        # Create a target with an intentional mismatch
        target = TestTarget([
            {'id': 1, 'name': 'John', 'age': 31},  # Age mismatch  
            {'id': 2, 'name': 'Jane', 'age': 25},
            {'id': 3, 'name': 'Jim', 'age': 35},
            {'id': 10, 'name': 'Janet', 'age': 40},
            {'id': 11, 'name': 'Jack', 'age': 46},  # Age mismatch
            {'id': 12, 'name': 'Jill', 'age': 50},
        ])
        
        # Initialize the engine with some pre-existing shadow data
        # This simulates a previous run
        self.engine.shadow.sql("""
            INSERT INTO shadow (pk, source_operation, target_operation, validation_error) VALUES
                (1, 2, 2, True),
                (2, 2, 2, True),
                (3, 2, 2, False),
                (5, 1, 1, False),
                (11, 2, 2, False);
        """)
        
        # Run validation with ID range 3-12
        metrics = self.engine.seatbelt_check(source, target, id_range=(3, 13))
        
        # Check that shadow is correctly updated:
        # - ID 1 should be in the shadow, but not updated
        # - ID 5 should be gone (not in source or target)
        # - IDs 3, 10, 11, 12 should be present (in range)
        shadow_data = self.engine.fetchall_shadow()
        self.assertEqual(len(shadow_data), 6)
        self.assertNotIn(5, shadow_data)
        for id in [1, 2]:
            self.assertIn(id, shadow_data)
            # TODO: assert that these rows weren't updated in the shadow
        for id in [3, 10, 11, 12]:
            self.assertIn(id, shadow_data)

        # ID 11 should have a pending status due to the age mismatch
        self.assertEqual(metrics['pending_count'], 1)


if __name__ == '__main__':
    unittest.main()
