from faker import Faker
import random
import hashlib
import json
import logging
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
    determine_destination_operation,
    check_for_validation_error
)

# Custom logging formatter that includes the current timestamp
class TimestampAdapter(logging.LoggerAdapter):
    def process(self, msg, kwargs):
        return f"current_ts={source_sequence_no} - {msg}", kwargs

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)
tracing_ids = []

# Set seeds for reproducibility
RANDOM_SEED = 42
random.seed(RANDOM_SEED)
fake = Faker()
fake.seed_instance(RANDOM_SEED)

source_db_log = []
source_db = {}  # Materialized view of source
target_db = {}
seatbelt = {}

# New variables to match tui.py capabilities
corrupt_filter = set()  # IDs that should be filtered out of the pipeline
corrupt_nulls = False   # Whether NULL values should be replaced with 0.0

source_sequence_no = 0
primary_key_sequence_no = 1
last_modified_row_id = None  # Track the most recently modified row

# Create the adapter after source_sequence_no is defined
logger = TimestampAdapter(logger, {})

staging = []
sync_state = {
    'last_extract_ts': -1,
    'last_load_ts': -1,
}

# Add metrics tracking similar to tui.py
metrics = {
    "lag": 0,
    "source_ops_count": 0,
    "target_ops_count": 0,
    "corruption_count": 0,
    "source_db_size": 0,
    "target_db_size": 0,
    "seatbelt_size": 0,
    "error_count": 0,
    "pending_count": 0,
    "valid_count": 0,
}

def insert_row():
    global source_sequence_no
    global primary_key_sequence_no
    global source_db
    global last_modified_row_id
    global metrics

    new_row = {
        'ts': source_sequence_no,
        'id': primary_key_sequence_no,
        'deleted': False,
        'name': fake.name(),
        'score': round(random.random() * 100, 2),
    }
    source_db_log.append(new_row)
    
    # Update materialized view
    source_db[primary_key_sequence_no] = new_row
    
    logger.debug(f"INSERT: id={primary_key_sequence_no}, ts={source_sequence_no}, name={new_row['name']}")
    source_sequence_no += 1
    primary_key_sequence_no += 1
    
    # Update metrics
    metrics["source_ops_count"] += 1
    metrics["source_db_size"] = len(source_db)
    
    # Calculate lag
    if sync_state['last_load_ts'] == -1:
        metrics["lag"] = source_sequence_no  # All operations are lag if nothing has been loaded
    else:
        metrics["lag"] = source_sequence_no - sync_state['last_load_ts']
    
    # Track as most recently modified row
    last_modified_row_id = new_row['id']

    return new_row['id']

def insert_with_null():
    """Insert a new row with a NULL score."""
    global source_sequence_no
    global primary_key_sequence_no
    global source_db
    global last_modified_row_id
    global metrics

    new_row = {
        'ts': source_sequence_no,
        'id': primary_key_sequence_no,
        'deleted': False,
        'name': fake.name(),
        'score': None,  # NULL score
    }
    source_db_log.append(new_row)
    
    # Update materialized view
    source_db[primary_key_sequence_no] = new_row
    
    logger.debug(f"INSERT: id={primary_key_sequence_no}, ts={source_sequence_no}, name={new_row['name']}, score=NULL")
    source_sequence_no += 1
    primary_key_sequence_no += 1
    
    # Update metrics
    metrics["source_ops_count"] += 1
    metrics["source_db_size"] = len(source_db)
    
    # Calculate lag
    if sync_state['last_load_ts'] == -1:
        metrics["lag"] = source_sequence_no
    else:
        metrics["lag"] = source_sequence_no - sync_state['last_load_ts']
    
    # Track as most recently modified row
    last_modified_row_id = new_row['id']

    return new_row['id']

def update_row():
    global source_sequence_no
    global source_db
    global last_modified_row_id
    global metrics

    if not source_db:
        logger.info("No rows to update")
        return None

    row_id = random.choice(list(source_db.keys()))
    original_row = source_db[row_id]
    new_row = original_row.copy()
    new_row['ts'] = source_sequence_no
    new_row['score'] = round(random.random() * 100, 2)
    source_db_log.append(new_row)
    
    # Update materialized view
    source_db[row_id] = new_row
    
    logger.debug(f"UPDATE: id={row_id}, ts={source_sequence_no}, old_score={original_row['score']}, new_score={new_row['score']}")
    source_sequence_no += 1
    
    # Update metrics
    metrics["source_ops_count"] += 1
    
    # Calculate lag
    if sync_state['last_load_ts'] == -1:
        metrics["lag"] = source_sequence_no
    else:
        metrics["lag"] = source_sequence_no - sync_state['last_load_ts']
    
    # Track as most recently modified row
    last_modified_row_id = row_id

    return row_id

def update_with_null():
    """Update a row with a NULL score."""
    global source_sequence_no
    global source_db
    global last_modified_row_id
    global metrics

    if not source_db:
        logger.info("No rows to update")
        return None

    row_id = random.choice(list(source_db.keys()))
    original_row = source_db[row_id]
    new_row = original_row.copy()
    new_row['ts'] = source_sequence_no
    new_row['score'] = None  # Set score to NULL
    source_db_log.append(new_row)
    
    # Update materialized view
    source_db[row_id] = new_row
    
    logger.debug(f"UPDATE: id={row_id}, ts={source_sequence_no}, old_score={original_row['score']}, new_score=NULL")
    source_sequence_no += 1
    
    # Update metrics
    metrics["source_ops_count"] += 1
    
    # Calculate lag
    if sync_state['last_load_ts'] == -1:
        metrics["lag"] = source_sequence_no
    else:
        metrics["lag"] = source_sequence_no - sync_state['last_load_ts']
    
    # Track as most recently modified row
    last_modified_row_id = row_id

    return row_id

def delete_row():
    global source_sequence_no
    global source_db
    global last_modified_row_id
    global metrics

    if not source_db:
        logger.info("No rows to delete")
        return None

    row_id = random.choice(list(source_db.keys()))
    source_db_log.append({
        'ts': source_sequence_no,
        'id': row_id,
        'deleted': True,
    })
    
    # Update materialized view
    source_db.pop(row_id, None)
    
    logger.debug(f"DELETE: id={row_id}, ts={source_sequence_no}")
    source_sequence_no += 1
    
    # Update metrics
    metrics["source_ops_count"] += 1
    metrics["source_db_size"] = len(source_db)
    
    # Calculate lag
    if sync_state['last_load_ts'] == -1:
        metrics["lag"] = source_sequence_no
    else:
        metrics["lag"] = source_sequence_no - sync_state['last_load_ts']
    
    # Since this row is deleted, it's not the most recently modified
    last_modified_row_id = None

    return row_id

def corrupt_by_update():
    """Update a row in the source and add its ID to the corrupt filter."""
    global corrupt_filter
    global metrics
    global source_sequence_no
    global source_db
    global last_modified_row_id

    # Safety check
    if len(source_db) == 0:
        logger.info("No rows to update in corrupt_by_update")
        return False

    # Choose a row
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

    # Calculate lag
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

    logger.info(f"UPDATE: id={row_id}, old_score={original_row['score']}, new_score={new_row['score']}")
    logger.info(f"CORRUPT FILTER: Added id={row_id} after update")

    return row_id

def corrupt_by_insert():
    """Insert a new row in the source and add its ID to the corrupt filter."""
    global corrupt_filter
    global metrics
    global source_sequence_no
    global source_db
    global last_modified_row_id
    global primary_key_sequence_no

    # Create a new row
    row_id = primary_key_sequence_no
    new_row = {
        'ts': source_sequence_no,
        'id': row_id,
        'deleted': False,
        'name': fake.name(),
        'score': round(random.random() * 100, 2),
    }

    # Add to operation log
    source_db_log.append(new_row)

    # Update materialized view
    source_db[row_id] = new_row

    # Update metadata
    source_sequence_no += 1
    primary_key_sequence_no += 1
    metrics["source_ops_count"] += 1
    metrics["source_db_size"] = len(source_db)

    # Calculate lag
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

    logger.info(f"INSERT: id={row_id}, name={new_row['name']}, score={new_row['score']}")
    logger.info(f"CORRUPT FILTER: Added id={row_id} after insert")

    return row_id

def corrupt_target_score():
    """Directly corrupt a random row in the target database by changing its score value."""
    global target_db
    global metrics
    
    # Safety check - make sure target DB has rows
    if len(target_db) == 0:
        logger.info("No rows in target database to corrupt")
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
    
    logger.info(f"TARGET CORRUPTED: id={row_id}, old_score={original_score}, new_score={new_score}")
    
    return row_id

def remove_from_filter():
    """Remove a random ID from the corrupt filter."""
    global corrupt_filter

    if not corrupt_filter:
        logger.info("No IDs in corrupt filter to remove")
        return False

    # Pick a random ID to remove
    row_id = random.choice(list(corrupt_filter))
    corrupt_filter.remove(row_id)
    logger.info(f"CORRUPT FILTER: Removed id={row_id}")
    return row_id

def toggle_null_corruption():
    """Toggle whether NULL values should be corrupted to 0.0 during loading."""
    global corrupt_nulls
    
    # Toggle the corrupt_nulls flag
    corrupt_nulls = not corrupt_nulls
    
    if corrupt_nulls:
        logger.info("NULL CORRUPTION: Enabled (NULL Mismap)")
    else:
        logger.info("NULL CORRUPTION: Disabled")
    
    return corrupt_nulls

def materialize_source():
    """
    Legacy function that's now redundant with the source_db dictionary
    Kept for backward compatibility
    """
    return source_db

def extract(up_to_ts=None):
    global sync_state
    global source_sequence_no
    global staging

    # If no timestamp is provided, use the current sequence number
    if up_to_ts is None:
        up_to_ts = source_sequence_no
        
    logger.debug(f"EXTRACT: from_ts={sync_state['last_extract_ts']}, to_ts={up_to_ts}")

    incremental = [
        row for row in source_db_log
        if row['ts'] > sync_state['last_extract_ts']
    ]
    staging.extend(incremental)
    logger.debug(f"EXTRACT: from_ts={sync_state['last_extract_ts']}, to_ts={up_to_ts}, rows_extracted={len(incremental)}")
    sync_state['last_extract_ts'] = up_to_ts
    source_sequence_no += 1

    for row in staging:
        if row['id'] in tracing_ids:
            logger.info(f"[TRACE] EXTRACT: id={row['id']}, ts={row['ts']}, deleted={row['deleted']}")
            
    return len(incremental)

def load():
    global staging
    global target_db
    global sync_state
    global corrupt_filter
    global corrupt_nulls
    global metrics

    logger.debug(f"LOAD: processing {len(staging)} rows")

    if not staging:
        logger.info("No data to load")
        return 0

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
            logger.info(f"FILTERED: id={row_id} (blocked by corrupt filter)")
            continue

        count += 1

        if row.get('deleted', False):
            deletes.add(row_id)
            target_db.pop(row_id, None)

            if row_id in tracing_ids:
                logger.info(f"[TRACE] LOAD - DELETE: id={row_id}, ts={row['ts']}, deleted={row['deleted']}")
        else:
            deletes.discard(row_id)
            # Create a copy without the 'deleted' field for target
            target_row = row.copy()
            if 'deleted' in target_row:
                target_row.pop('deleted')
            if 'ts' in target_row:
                target_row.pop('ts')

            # Check for NULL values in the score field and corrupt if necessary
            if corrupt_nulls and target_row.get('score') is None:
                target_row['score'] = 0.0  # Replace NULL with 0.0
                null_corrupted_count += 1
                logger.info(f"NULL CORRUPTED: id={row_id} (NULL Mismap)")

            target_db[row_id] = target_row

            if row_id in tracing_ids:
                logger.info(f"[TRACE] LOAD - UPSERT: id={row_id}, ts={row['ts']}, deleted={row['deleted']}")

    # Update sync state to mark when the last load occurred
    sync_state['last_load_ts'] = source_sequence_no

    # Update metrics
    metrics["target_ops_count"] += count
    metrics["target_db_size"] = len(target_db)
    if null_corrupted_count > 0:
        metrics["corruption_count"] += null_corrupted_count

    # Calculate lag
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

    logger.info(f"LOAD: {', '.join(status_parts)}")
    
    return count

def random_operation():
    if random.random() < 0.3:
        return insert_row()
    elif random.random() < 0.5:
        return update_row()
    else:
        return delete_row()

def seatbelt_check():
    global seatbelt
    global source_db
    global target_db
    global metrics

    source_db_signatures = {
        row['id']: row['ts'] for row in source_db.values()
    }
    target_db_signatures = {
        k: hashlib.sha256(json.dumps(v, sort_keys=True).encode()).hexdigest() 
        for k, v in target_db.items()
    }

    ids = set(source_db_signatures.keys()) | \
        set(target_db_signatures.keys()) | \
        set(seatbelt.keys())

    error_count = 0
    pending_count = 0
    valid_count = 0
    
    # Categorize discrepant IDs for more detailed reporting
    source_only_ids = []
    target_only_ids = []
    stale_ids = []

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
            source_row = source_db.get(id, {})
            target_row = target_db.get(id, {})

            # Compare score field's NULL state
            if 'score' in source_row and 'score' in target_row:
                source_is_null = source_row['score'] is None
                target_is_null = target_row['score'] is None
                if source_is_null != target_is_null:
                    null_mismatch = True

        # A row is considered to have an error when there's a NULL mismatch or validation error
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
            logger.info(f"[TRACE] SEATBELT CHECK: id={id}, source_operation={source_operation}, previous_source_operation={previous_source_operation}, target_operation={target_operation}, previous_target_operation={previous_target_operation}, previous_error={previous_error}, error={error}, null_mismatch={null_mismatch}")

        seatbelt[id] = {
            'source_signature': source_signature,
            'target_signature': target_signature,
            'source_operation': source_operation,
            'target_operation': target_operation,
            'validation_error': error,
            'null_mismatch': null_mismatch
        }

        if error:
            error_count += 1
            # Categorize the error
            if source_signature is not None and target_signature is None:
                source_only_ids.append(id)
            elif source_signature is None and target_signature is not None:
                target_only_ids.append(id)
            else:
                stale_ids.append(id)
                
            logger.error(f"Validation error for id={id} persists")
            logger.error(f"seatbelt error: id={id}, source_operation={source_operation}, previous_source_operation={previous_source_operation}, target_operation={target_operation}, previous_target_operation={previous_target_operation}, previous_error={previous_error}, error={error}, null_mismatch={null_mismatch}")
        elif source_operation not in [Operation.NOOP, Operation.DOES_NOT_EXIST] and target_operation in [Operation.NOOP, Operation.DOES_NOT_EXIST]:
            # Count rows that are present in source but not yet in target_db (or have a different state)
            pending_count += 1
        elif null_mismatch:
            # Count NULL mismatches as pending
            pending_count += 1
        elif source_signature is not None and target_signature is not None:
            # Count rows that are present in both source and target_db and have no errors
            valid_count += 1

    # Update metrics
    metrics.update({
        'source_db_size': len(source_db),
        'target_db_size': len(target_db),
        'seatbelt_size': len(seatbelt),
        'error_count': error_count,
        'pending_count': pending_count,
        'valid_count': valid_count,
    })
    
    # Display categorized errors
    if error_count > 0:
        if source_only_ids:
            source_only_str = ", ".join(str(id) for id in source_only_ids[:5])
            if len(source_only_ids) > 5:
                source_only_str += f" (and {len(source_only_ids) - 5} more)"
            logger.error(f"Source-Only Rows: {source_only_str}")
            
        if target_only_ids:
            target_only_str = ", ".join(str(id) for id in target_only_ids[:5])
            if len(target_only_ids) > 5:
                target_only_str += f" (and {len(target_only_ids) - 5} more)"
            logger.error(f"Target-Only Rows: {target_only_str}")
            
        if stale_ids:
            stale_str = ", ".join(str(id) for id in stale_ids[:5])
            if len(stale_ids) > 5:
                stale_str += f" (and {len(stale_ids) - 5} more)"
            logger.error(f"Drifted Rows: {stale_str}")
    
    logger.info(f"SEATBELT CHECK: valid={valid_count}, in-flight={pending_count}, discrepant={error_count}")
    logger.info(f"Metrics: {metrics}")
    
    return metrics

def find(id):
    return [row for row in source_db_log if row['id'] == id]

def trace(id):
    if id not in tracing_ids:
        tracing_ids.append(id)
        logger.info(f"[TRACE] Added tracing for id={id}")
    
    logger.info(f"[TRACE] source_db={[row for row in source_db_log if row['id'] == id]}")
    logger.info(f"[TRACE] target_db={target_db.get(id, None)}")
    return id

if __name__ == '__main__':
    # Seatbelt Orchestration Configuration
    seatbelt_interval = 25
    last_seatbelt_check_ts = 0
    seatbelt_check_sleep = 0

    # Create Plan
    plan = []
    for i in range(10):
        plan.append(lambda: trace(insert_row()))
    plan.append(lambda: extract(source_sequence_no))
    plan.append(lambda: load())

    # Add examples of the new operations
    plan.append(lambda: insert_with_null())
    plan.append(lambda: update_with_null())
    plan.append(lambda: corrupt_by_insert())
    plan.append(lambda: corrupt_by_update())
    plan.append(lambda: toggle_null_corruption())
    plan.append(lambda: extract())
    plan.append(lambda: load())
    plan.append(lambda: corrupt_target_score())
    plan.append(lambda: seatbelt_check())
    plan.append(lambda: remove_from_filter())
    
    # Regular operation cycle
    for i in range(20):
        for i in range(4):
            plan.append(lambda: trace(random_operation()))
        plan.append(lambda: extract(source_sequence_no))
        for i in range(2):
            plan.append(lambda: trace(random_operation()))
        plan.append(lambda: load())
        for i in range(5):
            plan.append(lambda: trace(random_operation()))

    # Execute Plan
    for step in plan:
        step()

        if source_sequence_no - last_seatbelt_check_ts > (seatbelt_interval + seatbelt_check_sleep):
            if sync_state['last_load_ts'] > last_seatbelt_check_ts:
                seatbelt_check()
                last_seatbelt_check_ts = source_sequence_no
                seatbelt_check_sleep = 0
            else:
                logger.info(f"Waiting for load to complete before next seatbelt check")
                seatbelt_check_sleep += 10

