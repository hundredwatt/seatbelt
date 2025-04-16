import curses
import time
import threading
from datetime import datetime
import copy
from curses import wrapper
from faker import Faker
import random
import logging
import sys
import hashlib
import json
import argparse
from typing import List, Dict, Any, Optional

# Import simulator components
from validation_logic import (
    Operation,
    DOES_NOT_EXIST,
    NOOP,
    INSERT,
    UPDATE,
    DELETE,
    INSERT_AND_UPDATE,
    UPDATE_AND_DELETE,
    TRANSIENT_UPDATE,
    determine_source_operation,
    check_for_validation_error
)

# Parse command-line arguments
parser = argparse.ArgumentParser(description='Data Validation TUI Simulator')
parser.add_argument('--check-only', action='store_true', help='Run initialization only and exit immediately (for error checking)')
args = parser.parse_args()

# Initialize Faker
fake = Faker()
fake.seed_instance(42)

# Global variables
source_db_log = []  # Log of all operations
source_db = {}      # Current state of source (materialized view)
target_db = {}      # id -> row
staging = []        # Temporary storage for extracted rows
logs = []           # Log messages
metrics = {
    "lag": 0,
    "source_ops_count": 0,
    "target_ops_count": 0,
    "corruption_count": 0  # Track how many times we've corrupted the target
}

sync_state = {
    'last_extract_ts': -1,
    'last_load_ts': -1,
}

# Seatbelt variables
seatbelt = {}  # Seatbelt state tracking
last_seatbelt_check_ts = -1
seatbelt_metrics = {
    "error_count": 0,
    "pending_count": 0,
    "valid_count": 0
}
tracing_ids = []  # IDs to trace through the system

source_sequence_no = 0
primary_key_sequence_no = 1
last_modified_row_id = None  # Track the most recently modified row
recently_loaded_ids = set()  # Track recently loaded row IDs for highlighting in Target DB
last_load_time = 0           # Track when the last load happened

# Lock for thread safety
lock = threading.Lock()

# Add new global variables for seatbelt animation
seatbelt_animation_state = {
    "active": False,
    "step": 0,
    "start_time": 0,
    "source_rows_read": 0,
    "target_rows_read": 0,
    "paused_until": 0,
    "completed": False,
    "new_metrics": {"error_count": 0, "pending_count": 0, "valid_count": 0}
}

# Add the corrupt_filter global variable after other global variables
global corrupt_filter
corrupt_filter = set()  # IDs that should be filtered out of the pipeline

# Add the null_corruption global variable to track if NULL values should be corrupted
global corrupt_nulls
corrupt_nulls = False  # Whether NULL values should be replaced with 0.0

# Add keyboard buffer
key_buffer = []  # List to store last 32 key presses
last_key_activity = time.time()  # Track when the last key was pressed

# Configure logging to capture messages
class TUILogHandler(logging.Handler):
    def __init__(self):
        super().__init__()

    def emit(self, record):
        msg = self.format(record)
        with lock:
            logs.append(msg)
            if len(logs) > 100:  # Keep only the last 100 logs
                logs.pop(0)

# Setup logging
logger = logging.getLogger("tui_simulator")
logger.setLevel(logging.INFO)
log_handler = TUILogHandler()
log_formatter = logging.Formatter('%(asctime)s - %(levelname)s - %(message)s')
log_handler.setFormatter(log_formatter)
logger.addHandler(log_handler)

# Operations
def insert_row() -> int:
    global source_sequence_no
    global primary_key_sequence_no
    global source_db
    global source_db_log
    global last_modified_row_id
    global metrics

    with lock:
        new_row = {
            'ts': source_sequence_no,
            'id': primary_key_sequence_no,
            'deleted': False,
            'name': fake.name(),
            'score': round(random.random() * 100, 2),
        }

        # Add to operation log
        source_db_log.append(new_row)

        # Update materialized view
        source_db[primary_key_sequence_no] = new_row

        # Update metadata
        source_sequence_no += 1
        primary_key_sequence_no += 1
        metrics["source_ops_count"] += 1

        # Calculate lag directly instead of calling recalculate_lag() which would try to acquire the lock again
        if sync_state['last_load_ts'] == -1:
            metrics["lag"] = source_sequence_no  # All operations are lag if nothing has been loaded
        else:
            metrics["lag"] = source_sequence_no - sync_state['last_load_ts']

        # Track as most recently modified row
        last_modified_row_id = new_row['id']

        # Log the operation
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - INSERT: id={new_row['id']}, name={new_row['name']}, score={new_row['score']}")

        return new_row['id']

def update_row() -> Optional[int]:
    global source_sequence_no
    global source_db
    global source_db_log
    global last_modified_row_id
    global metrics

    with lock:
        if not source_db:
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - No rows to update")
            return None

        row_id = random.choice(list(source_db.keys()))
        original_row = source_db[row_id]

        # Create updated row
        new_row = original_row.copy()
        new_row['ts'] = source_sequence_no
        new_row['score'] = round(random.random() * 100, 2)

        # Add to operation log
        source_db_log.append(new_row)

        # Update materialized view
        source_db[row_id] = new_row

        # Update metadata
        source_sequence_no += 1
        metrics["source_ops_count"] += 1

        # Calculate lag directly
        if sync_state['last_load_ts'] == -1:
            metrics["lag"] = source_sequence_no  # All operations are lag if nothing has been loaded
        else:
            metrics["lag"] = source_sequence_no - sync_state['last_load_ts']

        # Track as most recently modified row
        last_modified_row_id = row_id

        # Log the operation
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - UPDATE: id={row_id}, old_score={original_row['score']}, new_score={new_row['score']}")

        return row_id

def delete_row() -> Optional[int]:
    global source_sequence_no
    global source_db
    global source_db_log
    global last_modified_row_id
    global metrics

    with lock:
        if not source_db:
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - No rows to delete")
            return None

        row_id = random.choice(list(source_db.keys()))

        # Add delete operation to log
        source_db_log.append({
            'ts': source_sequence_no,
            'id': row_id,
            'deleted': True,
        })

        # Update materialized view
        source_db.pop(row_id)

        # Update metadata
        source_sequence_no += 1
        metrics["source_ops_count"] += 1

        # Calculate lag directly
        if sync_state['last_load_ts'] == -1:
            metrics["lag"] = source_sequence_no  # All operations are lag if nothing has been loaded
        else:
            metrics["lag"] = source_sequence_no - sync_state['last_load_ts']

        # Since this row is deleted, it's not the most recently modified
        last_modified_row_id = None

        # Log the operation
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - DELETE: id={row_id}")

        return row_id

def insert_with_null() -> int:
    """Insert a new row with a NULL score."""
    global source_sequence_no
    global primary_key_sequence_no
    global source_db
    global source_db_log
    global last_modified_row_id
    global metrics

    with lock:
        new_row = {
            'ts': source_sequence_no,
            'id': primary_key_sequence_no,
            'deleted': False,
            'name': fake.name(),
            'score': None,  # NULL score
        }

        # Add to operation log
        source_db_log.append(new_row)

        # Update materialized view
        source_db[primary_key_sequence_no] = new_row

        # Update metadata
        source_sequence_no += 1
        primary_key_sequence_no += 1
        metrics["source_ops_count"] += 1

        # Calculate lag directly
        if sync_state['last_load_ts'] == -1:
            metrics["lag"] = source_sequence_no
        else:
            metrics["lag"] = source_sequence_no - sync_state['last_load_ts']

        # Track as most recently modified row
        last_modified_row_id = new_row['id']

        # Log the operation
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - INSERT: id={new_row['id']}, name={new_row['name']}, score=NULL")

        return new_row['id']

def update_with_null() -> Optional[int]:
    """Update a row with a NULL score."""
    global source_sequence_no
    global source_db
    global source_db_log
    global last_modified_row_id
    global metrics

    with lock:
        if not source_db:
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - No rows to update")
            return None

        row_id = random.choice(list(source_db.keys()))
        original_row = source_db[row_id]

        # Create updated row
        new_row = original_row.copy()
        new_row['ts'] = source_sequence_no
        new_row['score'] = None  # Set score to NULL

        # Add to operation log
        source_db_log.append(new_row)

        # Update materialized view
        source_db[row_id] = new_row

        # Update metadata
        source_sequence_no += 1
        metrics["source_ops_count"] += 1

        # Calculate lag directly
        if sync_state['last_load_ts'] == -1:
            metrics["lag"] = source_sequence_no
        else:
            metrics["lag"] = source_sequence_no - sync_state['last_load_ts']

        # Track as most recently modified row
        last_modified_row_id = row_id

        # Log the operation
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - UPDATE: id={row_id}, old_score={original_row['score']}, new_score=NULL")

        return row_id

def extract():
    """Extract incremental changes from source_db_log since the last extract."""
    global staging
    global source_db
    global sync_state
    global source_sequence_no
    global metrics

    with lock:
        # Get incremental changes from source_db_log based on timestamp
        incremental = [
            row for row in source_db_log
            if row['ts'] > sync_state['last_extract_ts']
        ]

        # Add to staging area
        staging.extend(incremental)

        # Increment source_sequence_no
        source_sequence_no += 1

        # Update last_extract_ts to the current source_sequence_no - 1 (before the increment)
        # This ensures we don't skip operations with the current sequence number
        sync_state['last_extract_ts'] = source_sequence_no - 1

        # If nothing was found to extract and staging is still empty after adding incremental,
        # update last_load_ts as well to match source_sequence_no
        if not incremental and not staging:
            sync_state['last_load_ts'] = source_sequence_no
            # Recalculate lag (should be 0)
            metrics["lag"] = 0
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - EXTRACT: no changes found, updating sync state for implicit LOAD")
        else:
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - EXTRACT: {len(incremental)} operations extracted to staging")

def corrupt_by_update():
    """Update a row in the source and add its ID to the corrupt filter."""
    global corrupt_filter
    global metrics
    global source_sequence_no
    global source_db
    global source_db_log
    global last_modified_row_id

    try:
        # Using a non-blocking lock to prevent freezing
        if not lock.acquire(blocking=False):
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - WARNING: Could not acquire lock for corrupt_by_update")
            return False

        try:
            # Safety check
            if len(source_db) == 0:
                logs.append(f"{datetime.now().strftime('%H:%M:%S')} - No rows to update in corrupt_by_update")
                return False

            # Choose a row directly instead of calling update_row
            row_id = random.choice(list(source_db.keys()))
            original_row = source_db[row_id]

            # Create updated row
            new_row = original_row.copy()
            new_row['ts'] = source_sequence_no
            new_row['score'] = round(random.random() * 100, 2)

            # Add to operation log
            source_db_log.append(new_row)

            # Update materialized view
            source_db[row_id] = new_row

            # Update metadata
            source_sequence_no += 1
            metrics["source_ops_count"] += 1

            # Calculate lag directly
            if sync_state['last_load_ts'] == -1:
                metrics["lag"] = source_sequence_no
            else:
                metrics["lag"] = source_sequence_no - sync_state['last_load_ts']

            # Add the row ID to the corrupt filter
            corrupt_filter.add(row_id)

            # Increment corruption count
            metrics["corruption_count"] += 1

            # Track as most recently modified row
            last_modified_row_id = row_id

            # Log the operation
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - UPDATE: id={row_id}, old_score={original_row['score']}, new_score={new_row['score']}")
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - CORRUPT FILTER: Added id={row_id} after update")

            return True
        finally:
            lock.release()
    except Exception as e:
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - ERROR in corrupt_by_update: {str(e)}")
        # Make sure lock is released in case of error
        try:
            if lock._is_owned():
                lock.release()
        except Exception:
            pass  # Already released or couldn't release
        return False

def corrupt_by_insert():
    """Insert a new row in the source and add its ID to the corrupt filter."""
    global corrupt_filter
    global metrics
    global source_sequence_no
    global source_db
    global source_db_log
    global last_modified_row_id
    global primary_key_sequence_no

    try:
        # Using a non-blocking lock to prevent freezing
        if not lock.acquire(blocking=False):
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - WARNING: Could not acquire lock for corrupt_by_insert")
            return False

        try:
            # Create a new row directly instead of calling insert_row
            row_id = max([0] + list(source_db.keys())) + 1
            new_row = {
                'id': row_id,
                'name': f"Test User {row_id}",
                'score': round(random.random() * 100, 2),
                'ts': source_sequence_no
            }

            # Add to operation log
            source_db_log.append(new_row)

            # Update materialized view
            source_db[row_id] = new_row

            # Update metadata
            source_sequence_no += 1
            metrics["source_ops_count"] += 1

            # Update primary_key_sequence_no to be greater than the new row's ID
            # This ensures future regular inserts won't reuse the ID
            if row_id >= primary_key_sequence_no:
                primary_key_sequence_no = row_id + 1

            # Calculate lag directly
            if sync_state['last_load_ts'] == -1:
                metrics["lag"] = source_sequence_no
            else:
                metrics["lag"] = source_sequence_no - sync_state['last_load_ts']

            # Add the row ID to the corrupt filter
            corrupt_filter.add(row_id)

            # Increment corruption count
            metrics["corruption_count"] += 1

            # Track as most recently modified row
            last_modified_row_id = row_id

            # Log the operation
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - INSERT: id={row_id}, name={new_row['name']}, score={new_row['score']}")
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - CORRUPT FILTER: Added id={row_id} after insert")

            return True
        finally:
            lock.release()
    except Exception as e:
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - ERROR in corrupt_by_insert: {str(e)}")
        # Make sure lock is released in case of error
        try:
            if lock._is_owned():
                lock.release()
        except Exception:
            pass  # Already released or couldn't release
        return False

def remove_from_filter():
    """Remove a random ID from the corrupt filter."""
    global corrupt_filter

    try:
        # Using a non-blocking lock to prevent freezing
        if not lock.acquire(blocking=False):
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - WARNING: Could not acquire lock for remove_from_filter")
            return False

        try:
            if not corrupt_filter:
                logs.append(f"{datetime.now().strftime('%H:%M:%S')} - No IDs in corrupt filter to remove")
                return False

            # Pick a random ID to remove
            row_id = random.choice(list(corrupt_filter))
            corrupt_filter.remove(row_id)
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - CORRUPT FILTER: Removed id={row_id}")
            return True
        finally:
            lock.release()
    except Exception as e:
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - ERROR in remove_from_filter: {str(e)}")
        # Make sure lock is released in case of error
        try:
            if lock._is_owned():
                lock.release()
        except Exception:
            pass  # Already released or couldn't release
        return False

def toggle_null_corruption():
    """Toggle whether NULL values should be corrupted to 0.0 during loading."""
    global corrupt_nulls
    
    try:
        # Using a non-blocking lock to prevent freezing
        if not lock.acquire(blocking=False):
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - WARNING: Could not acquire lock for toggle_null_corruption")
            return False
        
        try:
            # Toggle the corrupt_nulls flag
            corrupt_nulls = not corrupt_nulls
            
            if corrupt_nulls:
                logs.append(f"{datetime.now().strftime('%H:%M:%S')} - NULL CORRUPTION: Enabled (NULL Mismap)")
            else:
                logs.append(f"{datetime.now().strftime('%H:%M:%S')} - NULL CORRUPTION: Disabled")
            
            return True
        finally:
            lock.release()
    except Exception as e:
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - ERROR in toggle_null_corruption: {str(e)}")
        # Make sure lock is released in case of error
        try:
            if lock._is_owned():
                lock.release()
        except Exception:
            pass  # Already released or couldn't release
        return False

def corrupt_target_score():
    """Directly corrupt a random row in the target database by changing its score value."""
    global target_db
    global metrics
    
    try:
        # Using a non-blocking lock to prevent freezing
        if not lock.acquire(blocking=False):
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - WARNING: Could not acquire lock for corrupt_target_score")
            return False
        
        try:
            # Safety check - make sure target DB has rows
            if len(target_db) == 0:
                logs.append(f"{datetime.now().strftime('%H:%M:%S')} - No rows in target database to corrupt")
                return False
            
            # Choose a random row ID from target DB
            row_id = random.choice(list(target_db.keys()))
            target_row = target_db[row_id]
            
            # Store the original score for logging
            original_score = target_row.get('score')
            
            # Generate a new, different score value
            if original_score is None:
                new_score = round(random.random() * 100, 2)  # Change NULL to a number
            else:
                # Ensure the new score is different from the original
                while True:
                    new_score = round(random.random() * 100, 2)
                    if abs(new_score - original_score) > 0.01:  # Ensure it's meaningfully different
                        break
            
            # Update the row with the corrupted score
            target_row['score'] = new_score
            target_db[row_id] = target_row
            
            # Increment corruption count
            metrics["corruption_count"] += 1
            
            # Log the operation
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - TARGET CORRUPTED: id={row_id}, old_score={original_score}, new_score={new_score}")
            
            return True
        finally:
            lock.release()
    except Exception as e:
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - ERROR in corrupt_target_score: {str(e)}")
        # Make sure lock is released in case of error
        try:
            if lock._is_owned():
                lock.release()
        except Exception:
            pass  # Already released or couldn't release
        return False

# Modify the load function to apply the corrupt filter
def load():
    """Load staged changes to the target database, filtering out corrupt IDs."""
    global staging
    global target_db
    global metrics
    global sync_state
    global source_sequence_no
    global recently_loaded_ids
    global last_load_time
    global corrupt_filter
    global corrupt_nulls

    with lock:
        if not staging:
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - No data to load")
            return

        # Clear the recently loaded IDs set before adding new ones
        recently_loaded_ids.clear()

        # Process all operations in staging
        deletes = set()
        count = 0
        filtered_count = 0
        null_corrupted_count = 0

        for row in staging:
            row_id = row['id']

            # Check if the ID is in the corrupt filter
            if row_id in corrupt_filter:
                filtered_count += 1
                logs.append(f"{datetime.now().strftime('%H:%M:%S')} - FILTERED: id={row_id} (blocked by corrupt filter)")
                continue

            count += 1

            if row.get('deleted', False):
                deletes.add(row_id)
                target_db.pop(row_id, None)
            else:
                deletes.discard(row_id)
                # Create a copy without the 'deleted' field for target
                target_row = row.copy()
                if 'deleted' in target_row:
                    target_row.pop('deleted')

                # Check for NULL values in the score field and corrupt if necessary
                if corrupt_nulls and target_row.get('score') is None:
                    target_row['score'] = 0.0  # Replace NULL with 0.0
                    null_corrupted_count += 1
                    logs.append(f"{datetime.now().strftime('%H:%M:%S')} - NULL CORRUPTED: id={row_id} (NULL Mismap)")

                target_db[row_id] = target_row
                # Add to recently loaded IDs for highlighting
                recently_loaded_ids.add(row_id)

        # Update timestamp for recently loaded rows highlight
        last_load_time = time.time()

        # Update sync state to mark when the last load occurred
        sync_state['last_load_ts'] = source_sequence_no

        # Update metrics
        metrics["target_ops_count"] += count
        if null_corrupted_count > 0:
            metrics["corruption_count"] += null_corrupted_count

        # Calculate lag directly
        if sync_state['last_load_ts'] == -1:
            metrics["lag"] = source_sequence_no  # All operations are lag if nothing has been loaded
        else:
            metrics["lag"] = source_sequence_no - sync_state['last_load_ts']

        # Clear staging area
        staging = []

        # Log the complete results
        status_parts = []
        if count > 0:
            status_parts.append(f"{count} operations loaded")
        if filtered_count > 0:
            status_parts.append(f"{filtered_count} filtered out")
        if null_corrupted_count > 0:
            status_parts.append(f"{null_corrupted_count} NULL values mismapped")

        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - LOAD: {', '.join(status_parts)}")

def seatbelt_check():
    """
    Run a seatbelt check to validate data consistency between source and target.
    This is similar to the seatbelt_check in 01_simulator.py but works directly with the target DB.
    """
    global seatbelt
    global source_db_log
    global target_db
    global last_seatbelt_check_ts
    global seatbelt_metrics
    global sync_state
    global seatbelt_animation_state

    try:
        with lock:
            # Check if we need to wait for a load operation
            if sync_state['last_load_ts'] <= last_seatbelt_check_ts:
                logs.append(f"{datetime.now().strftime('%H:%M:%S')} - Waiting for load to complete before next seatbelt check")
                return False

            # Start the animation sequence
            seatbelt_animation_state = {
                "active": True,
                "step": 1,  # Start at step 1 (Reading Source DB Signatures)
                "start_time": time.time(),
                "source_rows_read": len(source_db),
                "target_rows_read": len(target_db),
                "paused_until": time.time() + 0.5,  # Show step 1 for 0.5 second
                "completed": False,
                "new_metrics": {"error_count": 0, "pending_count": 0, "valid_count": 0}
            }

            # The actual seatbelt check will be completed in the next animation steps
            return True
    except Exception as e:
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - ERROR in seatbelt_check: {str(e)}")
        return False

def complete_seatbelt_check():
    """Complete the seatbelt check after animation completes"""
    global seatbelt
    global source_db_log
    global target_db
    global last_seatbelt_check_ts
    global seatbelt_metrics
    global seatbelt_animation_state

    try:
        with lock:
            # Materialize current source DB state for comparison
            source_db_materialized = {}
            for entry in source_db_log:
                if entry.get('deleted', False):
                    source_db_materialized.pop(entry['id'], None)
                else:
                    source_db_materialized[entry['id']] = entry

            # Get signatures for current state
            source_db_signatures = {
                row['id']: row['ts'] for row in source_db_materialized.values()
            }
            target_db_signatures = {
                k: hashlib.sha256(json.dumps(v, sort_keys=True).encode()).hexdigest()
                for k, v in target_db.items()
            }

            # Collect all IDs to check
            ids = set(source_db_signatures.keys()) | set(target_db_signatures.keys()) | set(seatbelt.keys())

            error_count = 0
            pending_count = 0
            valid_count = 0

            for id in ids:
                source_signature = source_db_signatures.get(id, None)
                target_signature = target_db_signatures.get(id, None)
                seatbelt_row = seatbelt.get(id, {})

                source_operation = determine_source_operation(source_signature, seatbelt_row.get('source_signature', None))
                target_operation = determine_source_operation(target_signature, seatbelt_row.get('target_signature', None))
                previous_source_operation = seatbelt_row.get('source_operation', None)
                previous_target_operation = seatbelt_row.get('target_operation', None)
                previous_error = seatbelt_row.get('validation_error', False)

                # Check NULL equivalence between source and target rows
                null_mismatch = False
                if source_signature is not None and target_signature is not None:
                    source_row = source_db_materialized.get(id, {})
                    target_row = target_db.get(id, {})

                    # Compare each column's NULL state
                    # For this example, we check only 'score' field
                    if 'score' in source_row and 'score' in target_row:
                        source_is_null = source_row['score'] is None
                        target_is_null = target_row['score'] is None
                        if source_is_null != target_is_null:
                            null_mismatch = True

                # A row is considered stale when source and target operations are NOOP
                # but there's a NULL equivalence mismatch
                if null_mismatch and source_operation == Operation.NOOP and target_operation == Operation.NOOP:
                    error = True
                else:
                    error = check_for_validation_error(
                        source_operation,
                        previous_source_operation,
                        target_operation,
                        previous_target_operation,
                        previous_error
                    )

                if id in tracing_ids:
                    logs.append(f"{datetime.now().strftime('%H:%M:%S')} - [TRACE] SEATBELT CHECK: id={id}, source_op={source_operation}, prev_source_op={previous_source_operation}, target_op={target_operation}, prev_target_op={previous_target_operation}, prev_error={previous_error}, error={error}, null_mismatch={null_mismatch}")

                seatbelt[id] = {
                    'source_signature': source_signature,
                    'target_signature': target_signature,
                    'source_operation': source_operation,
                    'target_operation': target_operation,
                    'validation_error': error,
                    'null_mismatch': null_mismatch  # Store the NULL mismatch status for reference
                }

                if error:
                    error_count += 1
                elif source_operation not in [Operation.NOOP, Operation.DOES_NOT_EXIST] and target_operation in [Operation.NOOP, Operation.DOES_NOT_EXIST]:
                    pending_count += 1
                elif null_mismatch:
                    pending_count += 1
                elif source_signature is not None and target_signature is not None:
                    # Count rows that are present in both source and target and have no errors
                    valid_count += 1

            # Store the new metrics in the animation state
            seatbelt_animation_state["new_metrics"] = {
                "error_count": error_count,
                "pending_count": pending_count,
                "valid_count": valid_count
            }

            # Update last check timestamp
            last_seatbelt_check_ts = source_sequence_no

            # Log metrics (but don't update seatbelt_metrics yet - that happens after animation completes)
            logs.append(f"{datetime.now().strftime('%H:%M:%S')} - SEATBELT CHECK: valid={valid_count}, in-flight={pending_count}, discrepant={error_count}")
    except Exception as e:
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - ERROR in complete_seatbelt_check: {str(e)}")
        # Reset animation state to avoid freezing
        seatbelt_animation_state["active"] = False
        seatbelt_animation_state["completed"] = True

def update_seatbelt_animation():
    """Update the seatbelt animation state based on timing"""
    global seatbelt_animation_state
    global seatbelt_metrics

    try:
        if not seatbelt_animation_state["active"]:
            return

        current_time = time.time()

        # If we're waiting for the next step
        if current_time < seatbelt_animation_state["paused_until"]:
            return

        # Advance to the next step
        current_step = seatbelt_animation_state["step"]

        if current_step == 1:  # Reading Source DB Signatures -> Reading Target DB Signatures
            seatbelt_animation_state["step"] = 2
            seatbelt_animation_state["paused_until"] = current_time + 0.5  # Show step 2 for 0.5 second

        elif current_step == 2:  # Reading Target DB Signatures -> Processing
            seatbelt_animation_state["step"] = 3
            # Do the actual processing now
            try:
                complete_seatbelt_check()
            except Exception as e:
                logs.append(f"{datetime.now().strftime('%H:%M:%S')} - ERROR in complete_seatbelt_check: {str(e)}")
                # Reset animation state to avoid getting stuck
                seatbelt_animation_state["active"] = False
                seatbelt_animation_state["completed"] = True
                return

            seatbelt_animation_state["paused_until"] = current_time + 0.5  # Show step 3 for 0.5 second

        elif current_step == 3:  # Processing -> Update Complete
            seatbelt_animation_state["step"] = 4
            # Now we can update the actual metrics
            with lock:
                seatbelt_metrics.update(seatbelt_animation_state["new_metrics"])
            seatbelt_animation_state["paused_until"] = current_time + 0.5  # Show completion for 0.5 second

        elif current_step == 4:  # Done with animation
            seatbelt_animation_state["active"] = False
            seatbelt_animation_state["completed"] = True
    except Exception as e:
        logs.append(f"{datetime.now().strftime('%H:%M:%S')} - ERROR in update_seatbelt_animation: {str(e)}")
        # Reset animation state to avoid getting stuck
        seatbelt_animation_state["active"] = False
        seatbelt_animation_state["completed"] = True

# TUI Components
def draw_box(stdscr, y, x, height, width, title=""):
    """Draw a box with an optional title."""
    max_y, max_x = stdscr.getmaxyx()

    # Ensure we don't try to draw outside the screen
    if y < 0 or x < 0 or y + height > max_y or x + width > max_x:
        # Adjust dimensions to fit within screen
        if y < 0: y = 0
        if x < 0: x = 0
        if y + height > max_y: height = max_y - y
        if x + width > max_x: width = max_x - x

        # Skip drawing if the box is too small
        if height < 3 or width < 3:
            return

    stdscr.attron(curses.color_pair(1))

    # Draw the box
    for i in range(y, y + height):
        if i < max_y:  # Check vertical boundary
            if x < max_x:  # Check horizontal boundary for left border
                try:
                    stdscr.addch(i, x, curses.ACS_VLINE)
                except curses.error:
                    pass  # Ignore errors when drawing at the edge

            if x + width - 1 < max_x:  # Check horizontal boundary for right border
                try:
                    stdscr.addch(i, x + width - 1, curses.ACS_VLINE)
                except curses.error:
                    pass  # Ignore errors when drawing at the edge

    for i in range(x, x + width):
        if i < max_x:  # Check horizontal boundary
            if y < max_y:  # Check vertical boundary for top border
                try:
                    stdscr.addch(y, i, curses.ACS_HLINE)
                except curses.error:
                    pass  # Ignore errors when drawing at the edge

            if y + height - 1 < max_y:  # Check vertical boundary for bottom border
                try:
                    stdscr.addch(y + height - 1, i, curses.ACS_HLINE)
                except curses.error:
                    pass  # Ignore errors when drawing at the edge

    # Draw corners
    if y < max_y and x < max_x:
        try:
            stdscr.addch(y, x, curses.ACS_ULCORNER)
        except curses.error:
            pass

    if y < max_y and x + width - 1 < max_x:
        try:
            stdscr.addch(y, x + width - 1, curses.ACS_URCORNER)
        except curses.error:
            pass

    if y + height - 1 < max_y and x < max_x:
        try:
            stdscr.addch(y + height - 1, x, curses.ACS_LLCORNER)
        except curses.error:
            pass

    if y + height - 1 < max_y and x + width - 1 < max_x:
        try:
            stdscr.addch(y + height - 1, x + width - 1, curses.ACS_LRCORNER)
        except curses.error:
            pass

    # Add title if provided
    if title and y < max_y and x + 2 < max_x:
        title_len = min(len(title), width - 4)
        try:
            stdscr.addstr(y, x + 2, f" {title[:title_len]} ")
        except curses.error:
            pass

    stdscr.attroff(curses.color_pair(1))

def add_str_safe(stdscr, y, x, text, color_pair=None):
    """Safely add a string to the screen, handling boundary errors."""
    max_y, max_x = stdscr.getmaxyx()
    if y < 0 or y >= max_y or x < 0 or x >= max_x:
        return

    # Truncate string to fit on screen
    if x + len(text) > max_x:
        text = text[:max_x - x]

    # Check if color_pair is actually an attribute like curses.A_BOLD
    is_attribute = isinstance(color_pair, int) and (color_pair & curses.A_ATTRIBUTES)

    if color_pair and not is_attribute:
        stdscr.attron(color_pair)
    elif is_attribute:
        stdscr.attron(color_pair)

    try:
        stdscr.addstr(y, x, text)
    except curses.error:
        pass  # Ignore errors at screen boundaries

    if color_pair and not is_attribute:
        stdscr.attroff(color_pair)
    elif is_attribute:
        stdscr.attroff(color_pair)

def draw_source_db(stdscr, y, x, height, width):
    """Draw the source database table."""
    draw_box(stdscr, y, x, height, width, "Source DB")

    # Table headers
    add_str_safe(stdscr, y + 1, x + 2, "ID | Name                 | Score")
    add_str_safe(stdscr, y + 2, x + 2, "-" * (width - 4))

    # Display rows (up to 8)
    with lock:
        rows = list(source_db.values())
        rows.sort(key=lambda r: r['id'], reverse=True)  # Show newest rows first

        for i, row in enumerate(rows[:8]):  # Show maximum 8 rows
            if y + 3 + i >= y + height - 1:
                break

            # Format score as NULL or a numeric value
            score_display = "NULL" if row['score'] is None else f"{row['score']:<5.2f}"
            row_str = f"{row['id']:<3} | {row['name'][:18]:<18} | {score_display}"

            # Only highlight the most recently modified row with green (only for update operations)
            # Last modified row ID is only set for update operations
            if row['id'] == last_modified_row_id:
                add_str_safe(stdscr, y + 3 + i, x + 2, row_str, curses.color_pair(2))
            else:
                add_str_safe(stdscr, y + 3 + i, x + 2, row_str)

def draw_target_db(stdscr, y, x, height, width):
    """Draw the target database table."""
    draw_box(stdscr, y, x, height, width, "Target DB")

    # Table headers
    add_str_safe(stdscr, y + 1, x + 2, "ID | Name                 | Score")
    add_str_safe(stdscr, y + 2, x + 2, "-" * (width - 4))

    # Display rows (all)
    with lock:
        rows = list(target_db.values())
        rows.sort(key=lambda r: r['id'], reverse=True)  # Show newest rows first

        for i, row in enumerate(rows):
            if y + 3 + i >= y + height - 1:
                break

            # Format score as NULL or a numeric value
            score_display = "NULL" if row['score'] is None else f"{row['score']:<5.2f}"
            row_str = f"{row['id']:<3} | {row['name'][:18]:<18} | {score_display}"

            # Highlight recently loaded rows in yellow (for 5 seconds after load)
            current_time = time.time()
            if row['id'] in recently_loaded_ids and current_time - last_load_time < 5:
                add_str_safe(stdscr, y + 3 + i, x + 2, row_str, curses.color_pair(3))  # Yellow for recently loaded
            else:
                add_str_safe(stdscr, y + 3 + i, x + 2, row_str)

def draw_pipeline(stdscr, y, x, height, width):
    """Draw the 2-stage replication pipeline."""
    draw_box(stdscr, y, x, height, width, "Pipeline")

    # Draw the flow
    mid_y = y + height // 2

    # Draw source → stage 1 (Extract)
    add_str_safe(stdscr, mid_y - 2, x + 5, "Source DB")

    # Draw arrow down from source
    try:
        stdscr.addch(mid_y - 1, x + 10, curses.ACS_VLINE)
    except curses.error:
        pass

    # Stage 1 (Extract)
    with lock:
        stage_1_status = f"Extract ({len(staging)} operations)"

    # Highlight extract stage with yellow if there are operations
    if len(staging) > 0:
        add_str_safe(stdscr, mid_y, x + 5, stage_1_status, curses.color_pair(3))
    else:
        add_str_safe(stdscr, mid_y, x + 5, stage_1_status)

    # Draw arrow down to load
    try:
        stdscr.addch(mid_y + 1, x + 10, curses.ACS_VLINE)
    except curses.error:
        pass

    # Stage 2 (Load)
    with lock:
        target_count = len(target_db)

    # Highlight target with cyan (border color)
    add_str_safe(stdscr, mid_y + 2, x + 5, f"Target DB ({target_count} rows)", curses.color_pair(1))

    # Draw pipeline status
    with lock:
        add_str_safe(stdscr, mid_y - 2, x + width - 20, "Pipeline Status:")
        if len(staging) > 0:
            add_str_safe(stdscr, mid_y, x + width - 20, f"READY TO LOAD: {len(staging)} operations", curses.color_pair(3))
        else:
            add_str_safe(stdscr, mid_y, x + width - 20, f"Last LOAD TS: {sync_state['last_load_ts']}")

        # Show lag
        if metrics["lag"] > 0:
            add_str_safe(stdscr, mid_y + 2, x + width - 20, f"LAG: {metrics['lag']} operations", curses.color_pair(4))

def draw_seatbelt(stdscr, y, x, width):
    """Draw the seatbelt component."""
    height = 15  # Increased height from 13 to 15 to fit the three new rows
    draw_box(stdscr, y, x, height, width, "Seatbelt")

    with lock:
        # If no seatbelt check has been run yet (last_seatbelt_check_ts is -1), show an empty box
        if last_seatbelt_check_ts == -1 and not seatbelt_animation_state["active"]:
            return

        # Always show the animation steps, whether active or completed
        if seatbelt_animation_state["active"] or seatbelt_animation_state["completed"]:
            current_step = seatbelt_animation_state["step"]

            # Display the steps with appropriate colors
            step1_color = curses.color_pair(3) if current_step == 1 else (curses.color_pair(2) if current_step > 1 else None)
            step2_color = curses.color_pair(3) if current_step == 2 else (curses.color_pair(2) if current_step > 2 else None)
            step3_color = curses.color_pair(3) if current_step == 3 else (curses.color_pair(2) if current_step > 3 else None)
            # Always use green for the completion status when animation is finished
            complete_color = curses.color_pair(2)

            # Draw the steps
            add_str_safe(stdscr, y + 1, x + 2, "1. Reading Source DB Signatures", step1_color)
            if current_step >= 1 or seatbelt_animation_state["completed"]:
                add_str_safe(stdscr, y + 2, x + 5, f"→ {seatbelt_animation_state['source_rows_read']} rows read", step1_color)

            add_str_safe(stdscr, y + 3, x + 2, "2. Reading Target DB Signatures", step2_color)
            if current_step >= 2 or seatbelt_animation_state["completed"]:
                add_str_safe(stdscr, y + 4, x + 5, f"→ {seatbelt_animation_state['target_rows_read']} rows read", step2_color)

            add_str_safe(stdscr, y + 5, x + 2, "3. Processing", step3_color)
            if current_step >= 4 or seatbelt_animation_state["completed"]:
                add_str_safe(stdscr, y + 6, x + 5, "→ State Updated", complete_color)

            # Add a blank line after the steps
            # Line y + 7 is blank

            # After the steps are complete or if it's done, show the metrics too
            if not seatbelt_animation_state["active"] or current_step >= 4:
                # Display last check timestamp
                add_str_safe(stdscr, y + 8, x + 2, f"Last Check TS: {last_seatbelt_check_ts}")

                # Display metrics with updated terminology and order
                # First line: Valid Rows and Rows In-Flight
                add_str_safe(stdscr, y + 9, x + 2, f"Valid Rows: {seatbelt_metrics['valid_count']}   Rows In-Flight: {seatbelt_metrics['pending_count']}")

                # Second line: Rows Discrepant - use warning symbol and bold if there are errors
                if seatbelt_metrics["error_count"] > 0:
                    discrepant_text = f"Rows Discrepant: {seatbelt_metrics['error_count']}"
                    add_str_safe(stdscr, y + 10, x + 2, discrepant_text, curses.A_BOLD)
                    add_str_safe(stdscr, y + 10, x + 2 + len(discrepant_text) + 1, "(!) ", curses.color_pair(4))
                else:
                    add_str_safe(stdscr, y + 10, x + 2, f"Rows Discrepant: {seatbelt_metrics['error_count']}")

                # Display errors categorized into three types if any
                if seatbelt_metrics["error_count"] > 0:
                    # Categorize discrepant IDs
                    source_only_ids = []
                    target_only_ids = []
                    stale_ids = []

                    for id, data in seatbelt.items():
                        if data.get('validation_error', False):
                            # Determine the category based on source and target signatures
                            source_sig = data.get('source_signature', None)
                            target_sig = data.get('target_signature', None)

                            # Check if this is a NULL mismatch
                            if source_sig is not None and target_sig is None:
                                # Exists in source but not in target
                                source_only_ids.append(id)
                            elif source_sig is None and target_sig is not None:
                                # Exists in target but not in source
                                target_only_ids.append(id)
                            else:
                                # Other validation errors (stale)
                                stale_ids.append(id)

                    # Display source-only rows
                    source_only_str = "Source-Only Rows: " + ", ".join(str(id) for id in source_only_ids[:5])
                    if len(source_only_ids) > 5:
                        source_only_str += f" (and {len(source_only_ids) - 5} more)"
                    add_str_safe(stdscr, y + 11, x + 2, source_only_str, curses.A_BOLD)

                    # Display target-only rows
                    target_only_str = "Target-Only Rows: " + ", ".join(str(id) for id in target_only_ids[:5])
                    if len(target_only_ids) > 5:
                        target_only_str += f" (and {len(target_only_ids) - 5} more)"
                    add_str_safe(stdscr, y + 12, x + 2, target_only_str, curses.A_BOLD)

                    # Display stale rows with NULL mismatch counts
                    stale_str = "Drifted Rows: " + ", ".join(str(id) for id in stale_ids[:5])
                    if len(stale_ids) > 5:
                        stale_str += f" (and {len(stale_ids) - 5} more)"
                    add_str_safe(stdscr, y + 13, x + 2, stale_str, curses.A_BOLD)
        else:
            # Initial state after at least one check has run
            # Display last check timestamp
            add_str_safe(stdscr, y + 1, x + 2, f"Last Check TS: {last_seatbelt_check_ts}")

            # Display metrics with updated terminology and order
            # First line: Valid Rows and Rows In-Flight
            add_str_safe(stdscr, y + 3, x + 2, f"Valid Rows: {seatbelt_metrics['valid_count']}   Rows In-Flight: {seatbelt_metrics['pending_count']}")

            # Second line: Rows Discrepant - use warning symbol and bold if there are errors
            if seatbelt_metrics["error_count"] > 0:
                discrepant_text = f"Rows Discrepant: {seatbelt_metrics['error_count']}"
                add_str_safe(stdscr, y + 4, x + 2, discrepant_text, curses.A_BOLD)
                add_str_safe(stdscr, y + 4, x + 2 + len(discrepant_text) + 1, "(!) ", curses.color_pair(4))
            else:
                add_str_safe(stdscr, y + 4, x + 2, f"Rows Discrepant: {seatbelt_metrics['error_count']}")

            # Display errors categorized into three types if any
            if seatbelt_metrics["error_count"] > 0:
                # Categorize discrepant IDs
                source_only_ids = []
                target_only_ids = []
                stale_ids = []

                for id, data in seatbelt.items():
                    if data.get('validation_error', False):
                        # Determine the category based on source and target signatures
                        source_sig = data.get('source_signature', None)
                        target_sig = data.get('target_signature', None)

                        # Check if this is a NULL mismatch
                        if source_sig is not None and target_sig is None:
                            # Exists in source but not in target
                            source_only_ids.append(id)
                        elif source_sig is None and target_sig is not None:
                            # Exists in target but not in source
                            target_only_ids.append(id)
                        else:
                            # Other validation errors (stale)
                            stale_ids.append(id)

                # Display source-only rows
                source_only_str = "Source-Only Rows: " + ", ".join(str(id) for id in source_only_ids[:5])
                if len(source_only_ids) > 5:
                    source_only_str += f" (and {len(source_only_ids) - 5} more)"
                add_str_safe(stdscr, y + 6, x + 2, source_only_str, curses.A_BOLD)

                # Display target-only rows
                target_only_str = "Target-Only Rows: " + ", ".join(str(id) for id in target_only_ids[:5])
                if len(target_only_ids) > 5:
                    target_only_str += f" (and {len(target_only_ids) - 5} more)"
                add_str_safe(stdscr, y + 7, x + 2, target_only_str, curses.A_BOLD)

                # Display stale rows with NULL mismatch counts
                stale_str = "Drifted Rows: " + ", ".join(str(id) for id in stale_ids[:5])
                if len(stale_ids) > 5:
                    stale_str += f" (and {len(stale_ids) - 5} more)"
                add_str_safe(stdscr, y + 8, x + 2, stale_str, curses.A_BOLD)

def draw_logs(stdscr, y, x, height, width):
    """Draw the log messages."""
    draw_box(stdscr, y, x, height, width, "Logs")

    # Display the most recent logs
    with lock:
        log_entries = logs[-height+2:]
        for i, log in enumerate(log_entries):
            if y + 1 + i < y + height - 1:
                # Truncate log to fit width
                log_display = log[-width+4:] if len(log) > width-4 else log

                # Set color based on log content
                color_pair = None
                if "INSERT" in log_display:
                    color_pair = curses.color_pair(5)  # Blue for inserts
                elif "UPDATE" in log_display:
                    color_pair = curses.color_pair(2)  # Green for updates
                elif "DELETE" in log_display:
                    color_pair = curses.color_pair(6)  # Purple for deletes
                elif "TARGET CORRUPTED" in log_display:
                    color_pair = curses.color_pair(4)  # Red for corruption
                elif "EXTRACT" in log_display or "LOAD" in log_display:
                    color_pair = curses.color_pair(3)  # Yellow for pipeline operations

                add_str_safe(stdscr, y + 1 + i, x + 2, log_display, color_pair)

def draw_metrics(stdscr, y, x, height, width):
    """Draw the metrics."""
    draw_box(stdscr, y, x, height, width, "Metrics")

    with lock:
        # Display metrics
        add_str_safe(stdscr, y + 1, x + 2, f"Lag: {metrics['lag']} operations")
        add_str_safe(stdscr, y + 2, x + 2, f"Source Ops: {metrics['source_ops_count']}")
        add_str_safe(stdscr, y + 3, x + 2, f"Target Ops: {metrics['target_ops_count']}")
        add_str_safe(stdscr, y + 4, x + 2, f"Staging: {len(staging)} operations")
        add_str_safe(stdscr, y + 5, x + 2, f"Source DB Size: {len(source_db)}")
        add_str_safe(stdscr, y + 6, x + 2, f"Target DB Size: {len(target_db)}")

        # Use warning symbol and bold if there are corruptions
        if metrics['corruption_count'] > 0:
            corruption_text = f"Corruptions: {metrics['corruption_count']}"
            add_str_safe(stdscr, y + 7, x + 2, corruption_text, curses.A_BOLD)
            add_str_safe(stdscr, y + 7, x + 2 + len(corruption_text) + 1, "(!) ", curses.color_pair(4))
        else:
            add_str_safe(stdscr, y + 7, x + 2, f"Corruptions: {metrics['corruption_count']}")

        # Add a blank line after Corruptions
        # Line y + 8 is now blank

        # Add a section header for timestamps
        add_str_safe(stdscr, y + 9, x + 2, "Timestamps:", curses.color_pair(1))
        add_str_safe(stdscr, y + 10, x + 2, f"Current TS: {source_sequence_no}")
        add_str_safe(stdscr, y + 11, x + 2, f"Last Extract TS: {sync_state['last_extract_ts']}")
        add_str_safe(stdscr, y + 12, x + 2, f"Last Load TS: {sync_state['last_load_ts']}")
        add_str_safe(stdscr, y + 13, x + 2, f"Last Seatbelt TS: {last_seatbelt_check_ts}")

        # Add a section header for seatbelt metrics
        add_str_safe(stdscr, y + 15, x + 2, "Seatbelt Metrics:", curses.color_pair(1))

        # Show valid rows count first
        add_str_safe(stdscr, y + 16, x + 2, f"Valid Rows: {seatbelt_metrics['valid_count']}")

        # Show pending count without color highlighting
        add_str_safe(stdscr, y + 17, x + 2, f"Rows In-Flight: {seatbelt_metrics['pending_count']}")

        # Show error count with warning symbol and bold if there are errors
        if seatbelt_metrics["error_count"] > 0:
            discrepant_text = f"Rows Discrepant: {seatbelt_metrics['error_count']}"
            add_str_safe(stdscr, y + 18, x + 2, discrepant_text, curses.A_BOLD)
            add_str_safe(stdscr, y + 18, x + 2 + len(discrepant_text) + 1, "(!) ", curses.color_pair(4))
        else:
            add_str_safe(stdscr, y + 18, x + 2, f"Rows Discrepant: {seatbelt_metrics['error_count']}")

def draw_corrupt_filter(stdscr, y, x, height, width, title="Corruption"):
    """Draw the corrupt filter box showing filtered IDs."""
    with lock:
        # Choose border color based on whether there are any IDs in the filter or NULL corruption is enabled
        border_color = curses.color_pair(4) if corrupt_filter or corrupt_nulls else curses.color_pair(1)  # Red if active, otherwise cyan

        # Set the title with appropriate emoji based on filter state
        if corrupt_filter or corrupt_nulls:
            display_title = "Corruption 😈"  # Evil emoji when active
        else:
            display_title = "Corruption 😴"  # Sleeping emoji when inactive

    # Draw box with specified border color
    max_y, max_x = stdscr.getmaxyx()

    # Ensure we don't try to draw outside the screen
    if y < 0 or x < 0 or y + height > max_y or x + width > max_x:
        # Adjust dimensions to fit within screen
        if y < 0: y = 0
        if x < 0: x = 0
        if y + height > max_y: height = max_y - y
        if x + width > max_x: width = max_x - x

        # Skip drawing if the box is too small
        if height < 3 or width < 3:
            return

    stdscr.attron(border_color)

    # Draw the box
    for i in range(y, y + height):
        if i < max_y:  # Check vertical boundary
            if x < max_x:  # Check horizontal boundary for left border
                try:
                    stdscr.addch(i, x, curses.ACS_VLINE)
                except curses.error:
                    pass  # Ignore errors when drawing at the edge

            if x + width - 1 < max_x:  # Check horizontal boundary for right border
                try:
                    stdscr.addch(i, x + width - 1, curses.ACS_VLINE)
                except curses.error:
                    pass  # Ignore errors when drawing at the edge

    for i in range(x, x + width):
        if i < max_x:  # Check horizontal boundary
            if y < max_y:  # Check vertical boundary for top border
                try:
                    stdscr.addch(y, i, curses.ACS_HLINE)
                except curses.error:
                    pass  # Ignore errors when drawing at the edge

            if y + height - 1 < max_y:  # Check vertical boundary for bottom border
                try:
                    stdscr.addch(y + height - 1, i, curses.ACS_HLINE)
                except curses.error:
                    pass  # Ignore errors when drawing at the edge

    # Draw corners
    if y < max_y and x < max_x:
        try:
            stdscr.addch(y, x, curses.ACS_ULCORNER)
        except curses.error:
            pass

    if y < max_y and x + width - 1 < max_x:
        try:
            stdscr.addch(y, x + width - 1, curses.ACS_URCORNER)
        except curses.error:
            pass

    if y + height - 1 < max_y and x < max_x:
        try:
            stdscr.addch(y + height - 1, x, curses.ACS_LLCORNER)
        except curses.error:
            pass

    if y + height - 1 < max_y and x + width - 1 < max_x:
        try:
            stdscr.addch(y + height - 1, x + width - 1, curses.ACS_LRCORNER)
        except curses.error:
            pass

    # Add title if provided
    with lock:
        if display_title and y < max_y and x + 2 < max_x:
            title_len = min(len(display_title), width - 4)
            try:
                stdscr.addstr(y, x + 2, f" {display_title[:title_len]} ")
            except curses.error:
                pass

    stdscr.attroff(border_color)

    with lock:
        # Show the number of IDs in the filter
        add_str_safe(stdscr, y + 1, x + 2, f"Blocked IDs: {len(corrupt_filter)}")

        # Add a separator line
        add_str_safe(stdscr, y + 2, x + 2, "-" * (width - 4))

        # List the IDs vertically
        sorted_ids = sorted(list(corrupt_filter))
        max_display_ids = height - 6  # Reserve space for header, blocked IDs count, separator, NULL status and bottom border

        for i, id in enumerate(sorted_ids[:max_display_ids]):
            if y + 3 + i >= y + height - 2:  # Leave space for NULL status line at bottom
                break

            add_str_safe(stdscr, y + 3 + i, x + 2, f"ID: {id}", curses.color_pair(4))

        # If we can't fit all IDs, show a count of remaining ones
        if len(sorted_ids) > max_display_ids:
            remaining = len(sorted_ids) - max_display_ids
            add_str_safe(stdscr, y + height - 3, x + 2, f"+ {remaining} more...", curses.color_pair(4))

        # Show NULL corruption status at the bottom
        null_status = "ON" if corrupt_nulls else "OFF"
        null_color = curses.color_pair(4) if corrupt_nulls else None  # Red if enabled
        add_str_safe(stdscr, y + height - 2, x + 2, f"NULL Mismap: {null_status}", null_color)

def draw_help(stdscr, y, x, width):
    """Draw keyboard controls help split into two lines."""
    # Split the help text into two lines
    help_line1 = "i: Insert | u: Update | d: Delete | I/U: Insert/Update w/ NULL | ^i/u: Corrupt Insert/Update"
    help_line2 = "^x: Corrupt Target Score | e: Extract | l: Load | s: Seatbelt | r: Remove Filter | n: Toggle NULL Mismap | q: Quit"
    
    add_str_safe(stdscr, y, x, help_line1[:width])
    add_str_safe(stdscr, y+1, x, help_line2[:width])

def main(stdscr):
    try:
        global source_sequence_no
        global primary_key_sequence_no
        global source_db
        global source_db_log
        global last_modified_row_id
        global metrics
        global sync_state
        global seatbelt
        global last_seatbelt_check_ts
        global seatbelt_metrics
        global seatbelt_animation_state
        global corrupt_filter
        global key_buffer  # Add key_buffer to globals

        # Initialize curses
        curses.curs_set(0)  # Hide cursor
        stdscr.clear()

        # Setup colors
        curses.start_color()
        curses.init_pair(1, curses.COLOR_CYAN, curses.COLOR_BLACK)    # Border color
        curses.init_pair(2, curses.COLOR_GREEN, curses.COLOR_BLACK)   # Highlight color for recently updated rows
        curses.init_pair(3, curses.COLOR_YELLOW, curses.COLOR_BLACK)  # Warning/Extraction
        curses.init_pair(4, curses.COLOR_RED, curses.COLOR_BLACK)     # Error/Corruption
        curses.init_pair(5, curses.COLOR_BLUE, curses.COLOR_BLACK)    # Info/Insert
        curses.init_pair(6, curses.COLOR_MAGENTA, curses.COLOR_BLACK)  # Purple for Delete

        # Enable keypad and nodelay mode
        stdscr.keypad(True)
        stdscr.nodelay(True)

        # Add some initial data - done outside the main loop to avoid blocking
        with lock:
            for _ in range(3):
                new_row = {
                    'ts': source_sequence_no,
                    'id': primary_key_sequence_no,
                    'deleted': False,
                    'name': fake.name(),
                    'score': round(random.random() * 100, 2),
                }

                # Add to operation log
                source_db_log.append(new_row)

                # Update materialized view
                source_db[primary_key_sequence_no] = new_row

                # Update metadata
                source_sequence_no += 1
                primary_key_sequence_no += 1
                metrics["source_ops_count"] += 1

                # Calculate lag directly
                if sync_state['last_load_ts'] == -1:
                    metrics["lag"] = source_sequence_no
                else:
                    metrics["lag"] = source_sequence_no - sync_state['last_load_ts']

                logs.append(f"{datetime.now().strftime('%H:%M:%S')} - INSERT: id={new_row['id']}, name={new_row['name']}, score={new_row['score']}")

            # Set the last modified row to be the most recent one
            last_modified_row_id = primary_key_sequence_no - 1

        # Get terminal dimensions
        max_y, max_x = stdscr.getmaxyx()

        # If --check-only flag is provided, exit immediately
        if args.check_only:
            print("Initialization successful. Exiting.")
            return  # Exit main function

        # Initialize state tracking
        last_redraw_time = 0
        last_source_ops = metrics["source_ops_count"]
        last_target_ops = metrics["target_ops_count"]
        last_staging_size = len(staging)
        last_terminal_size = (max_y, max_x)

        # Main loop
        running = True
        while running:
            current_time = time.time()

            # Check if we need to redraw the screen
            needs_redraw = False

            # Update seatbelt animation if active
            if seatbelt_animation_state["active"]:
                old_step = seatbelt_animation_state["step"]
                update_seatbelt_animation()
                if old_step != seatbelt_animation_state["step"]:
                    needs_redraw = True

            # Get current dimensions in case window was resized
            max_y, max_x = stdscr.getmaxyx()
            if (max_y, max_x) != last_terminal_size:
                needs_redraw = True
                last_terminal_size = (max_y, max_x)

            # Check if data has changed
            with lock:
                source_ops = metrics["source_ops_count"]
                target_ops = metrics["target_ops_count"]
                staging_size = len(staging)

            if (source_ops != last_source_ops or
                target_ops != last_target_ops or
                staging_size != last_staging_size):
                needs_redraw = True
                last_source_ops = source_ops
                last_target_ops = target_ops
                last_staging_size = staging_size

            # Force redraw every 1 second even if nothing changed
            if current_time - last_redraw_time > 1.0:
                needs_redraw = True

            # Process keyboard input
            try:
                key = stdscr.getch()
                if key != -1:  # -1 is returned when no key is pressed in nodelay mode
                    needs_redraw = True  # Always redraw when a key is pressed

                    if key == ord('q'):
                        running = False
                    elif key == ord('i'):
                        insert_row()
                        # Add to key buffer
                        key_buffer.append('i')
                        if len(key_buffer) > 32:  # Increased from 10 to 32
                            key_buffer.pop(0)
                        last_key_activity = time.time()  # Update activity timestamp
                    elif key == ord('u'):
                        if update_row() is not None:
                            # Add to key buffer
                            key_buffer.append('u')
                            if len(key_buffer) > 32:  # Increased from 10 to 32
                                key_buffer.pop(0)
                            last_key_activity = time.time()  # Update activity timestamp
                    elif key == ord('d'):
                        if delete_row() is not None:
                            # Add to key buffer
                            key_buffer.append('d')
                            if len(key_buffer) > 32:  # Increased from 10 to 32
                                key_buffer.pop(0)
                            last_key_activity = time.time()  # Update activity timestamp
                    elif key == ord('e'):
                        extract()
                        # Add to key buffer
                        key_buffer.append('e')
                        if len(key_buffer) > 32:  # Increased from 10 to 32
                            key_buffer.pop(0)
                        last_key_activity = time.time()  # Update activity timestamp
                    elif key == ord('l'):
                        load()
                        # Add to key buffer
                        key_buffer.append('l')
                        if len(key_buffer) > 32:  # Increased from 10 to 32
                            key_buffer.pop(0)
                        last_key_activity = time.time()  # Update activity timestamp
                    elif key == ord('s'):
                        # Add to key buffer regardless of whether the command executes
                        key_buffer.append('s')
                        if len(key_buffer) > 32:  # Increased from 10 to 32
                            key_buffer.pop(0)
                        last_key_activity = time.time()  # Update activity timestamp
                        # Run seatbelt check if possible
                        if not seatbelt_animation_state["active"]:
                            seatbelt_check()
                    elif key == 21:  # CTRL-u (21 is the ASCII code for CTRL-u)
                        # Add to key buffer regardless of whether the command executes
                        key_buffer.append('^u')  # Use ^u to indicate CTRL-u
                        if len(key_buffer) > 32:  # Increased from 10 to 32
                            key_buffer.pop(0)
                        last_key_activity = time.time()  # Update activity timestamp
                        # Run corrupt update if possible
                        corrupt_by_update()
                    elif key == 24:  # CTRL-x 
                        # Add to key buffer regardless of whether the command executes
                        key_buffer.append('^x')  # Use ^x to indicate CTRL-x
                        if len(key_buffer) > 32:
                            key_buffer.pop(0)
                        last_key_activity = time.time()  # Update activity timestamp
                        # Corrupt a random row in the target DB
                        corrupt_target_score()
                    elif key == 9:  # CTRL-i (9 is the ASCII code for CTRL-i)
                        # Add to key buffer regardless of whether the command executes
                        key_buffer.append('^i')  # Use ^i to indicate CTRL-i
                        if len(key_buffer) > 32:  # Increased from 10 to 32
                            key_buffer.pop(0)
                        last_key_activity = time.time()  # Update activity timestamp
                        # Run corrupt insert
                        corrupt_by_insert()
                    elif key == ord('r'):
                        # Add to key buffer regardless of whether the command executes
                        key_buffer.append('r')
                        if len(key_buffer) > 32:  # Increased from 10 to 32
                            key_buffer.pop(0)
                        last_key_activity = time.time()  # Update activity timestamp
                        # Run remove from filter
                        remove_from_filter()
                    elif key == ord('n'):
                        # Add to key buffer regardless of whether the command executes
                        key_buffer.append('n')
                        if len(key_buffer) > 32:  # Increased from 10 to 32
                            key_buffer.pop(0)
                        last_key_activity = time.time()  # Update activity timestamp
                        # Run toggle NULL corruption
                        toggle_null_corruption()
                    elif key == ord('I'):  # Capital I for NULL insert
                        # Add to key buffer regardless of whether the command executes
                        key_buffer.append('I')
                        if len(key_buffer) > 32:  # Increased from 10 to 32
                            key_buffer.pop(0)
                        last_key_activity = time.time()  # Update activity timestamp
                        # Run NULL insert
                        insert_with_null()
                    elif key == ord('U'):  # Capital U for NULL update
                        # Add to key buffer regardless of whether the command executes
                        key_buffer.append('U')
                        if len(key_buffer) > 32:  # Increased from 10 to 32
                            key_buffer.pop(0)
                        last_key_activity = time.time()  # Update activity timestamp
                        # Run NULL update
                        update_with_null()
            except Exception as e:
                # Display any errors that might occur
                key_buffer.append('!')  # Add error indicator to buffer
                if len(key_buffer) > 10:
                    key_buffer.pop(0)
                needs_redraw = True

            # Only redraw if necessary
            if needs_redraw:
                stdscr.clear()

                # Calculate panel dimensions
                top_height = 12  # Source/Pipeline/Target/Filter height
                seatbelt_height = 15  # Increased from 13 to 15 to accommodate all three row types

                # Calculate widths with Source DB, Pipeline, and Target DB having equal width
                # and Corrupt Filter having half their width
                main_panel_count = 3  # Source, Pipeline, Target
                main_panel_width = (max_x * 6) // (main_panel_count * 6 + 3)  # 6 parts for each main panel, 3 parts for filter
                filter_panel_width = main_panel_width // 2  # Filter is half the width

                # Adjust log height to leave space for keyboard buffer and help
                log_height = max_y - top_height - seatbelt_height - 3  # Reduced by 3 to leave space for keyboard buffer and 2-line help
                metrics_width = 30

                # Draw panels
                source_x = 0
                pipeline_x = source_x + main_panel_width
                filter_x = pipeline_x + main_panel_width
                target_x = filter_x + filter_panel_width

                draw_source_db(stdscr, 0, source_x, top_height, main_panel_width)
                draw_pipeline(stdscr, 0, pipeline_x, top_height, main_panel_width)
                # Add corrupt filter between Pipeline and Target with half width
                draw_corrupt_filter(stdscr, 0, filter_x, top_height, filter_panel_width)
                draw_target_db(stdscr, 0, target_x, top_height, main_panel_width)

                # Draw seatbelt panel (centered below pipeline)
                draw_seatbelt(stdscr, top_height, pipeline_x, main_panel_width)

                # Draw logs and metrics
                draw_logs(stdscr, top_height + seatbelt_height, 0, log_height, max_x - metrics_width)
                draw_metrics(stdscr, top_height + seatbelt_height, max_x - metrics_width, log_height, metrics_width)

                # Draw help at the bottom (now 2 lines)
                draw_help(stdscr, max_y - 2, 0, max_x)

                # Display key buffer (if any keys have been pressed)
                if key_buffer:
                    # Check if we should clear the buffer due to inactivity
                    if current_time - last_key_activity > 30:  # 30 seconds timeout
                        key_buffer.clear()
                        needs_redraw = True
                    else:
                        # Pad the buffer with spaces to always show 32 characters
                        buffer_display = ''.join(key_buffer).ljust(32)
                        add_str_safe(stdscr, max_y - 3, 0, buffer_display, curses.color_pair(3))  # Yellow color for visibility

                # Refresh screen
                stdscr.refresh()
                last_redraw_time = current_time

            # Sleep to reduce CPU usage
            time.sleep(0.05)  # Smaller delay for responsiveness but not too much CPU
    except Exception as e:
        # If a critical error occurs, exit curses mode and print the error
        curses.endwin()
        print(f"Critical error occurred: {str(e)}")
        import traceback
        traceback.print_exc()

if __name__ == "__main__":
    # Run the TUI
    try:
        wrapper(main)
    except Exception as e:
        print(f"Fatal error: {str(e)}")
        import traceback
        traceback.print_exc()
