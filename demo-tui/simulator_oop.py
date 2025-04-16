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

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)

class MetricsTracker:
    """Class responsible for tracking and reporting metrics"""
    
    def __init__(self):
        self.metrics = {
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
    
    def update(self, **kwargs):
        """Update multiple metrics at once"""
        self.metrics.update(kwargs)
    
    def increment(self, key, value=1):
        """Increment a specific metric"""
        self.metrics[key] += value
    
    def set(self, key, value):
        """Set a specific metric"""
        self.metrics[key] = value
    
    def get(self, key=None):
        """Get a specific metric or all metrics"""
        if key:
            return self.metrics.get(key, 0)
        return self.metrics
    
    def calculate_lag(self, source_sequence_no, sync_state):
        """Calculate and update the lag metric"""
        if sync_state['last_load_ts'] == -1:
            self.metrics["lag"] = source_sequence_no
        else:
            self.metrics["lag"] = source_sequence_no - sync_state['last_load_ts']

class Database:
    """Class responsible for database operations"""
    
    def __init__(self, random_seed=42):
        # Set seeds for reproducibility
        self.random_seed = random_seed
        random.seed(self.random_seed)
        self.fake = Faker()
        self.fake.seed_instance(self.random_seed)
        
        # Initialize database structures
        self.source_db_log = []
        self.source_db = {}  # Materialized view of source
        self.target_db = {}
        
        # Initialize sequence counters
        self.source_sequence_no = 0
        self.primary_key_sequence_no = 1
        self.last_modified_row_id = None
    
    def insert_row(self, metrics_tracker, sync_state, score=None):
        """Insert a new row into the source database"""
        new_row = {
            'ts': self.source_sequence_no,
            'id': self.primary_key_sequence_no,
            'deleted': False,
            'name': self.fake.name(),
            'score': round(random.random() * 100, 2) if score is None else score,
        }
        self.source_db_log.append(new_row)
        
        # Update materialized view
        self.source_db[self.primary_key_sequence_no] = new_row
        
        log_msg = f"INSERT: id={self.primary_key_sequence_no}, ts={self.source_sequence_no}, name={new_row['name']}"
        if score is None:
            pass  # Use default log
        else:
            log_msg += f", score={score}"
            
        logging.debug(log_msg)
        self.source_sequence_no += 1
        self.primary_key_sequence_no += 1
        
        # Update metrics
        metrics_tracker.increment("source_ops_count")
        metrics_tracker.set("source_db_size", len(self.source_db))
        metrics_tracker.calculate_lag(self.source_sequence_no, sync_state)
        
        # Track as most recently modified row
        self.last_modified_row_id = new_row['id']
        
        return new_row['id']
    
    def insert_with_null(self, metrics_tracker, sync_state):
        """Insert a new row with a NULL score"""
        return self.insert_row(metrics_tracker, sync_state, score=None)
    
    def update_row(self, metrics_tracker, sync_state, score=None):
        """Update an existing row in the source database"""
        if not self.source_db:
            logging.info("No rows to update")
            return None
            
        row_id = random.choice(list(self.source_db.keys()))
        original_row = self.source_db[row_id]
        new_row = original_row.copy()
        new_row['ts'] = self.source_sequence_no
        new_row['score'] = round(random.random() * 100, 2) if score is None else score
        self.source_db_log.append(new_row)
        
        # Update materialized view
        self.source_db[row_id] = new_row
        
        logging.debug(f"UPDATE: id={row_id}, ts={self.source_sequence_no}, old_score={original_row['score']}, new_score={new_row['score']}")
        self.source_sequence_no += 1
        
        # Update metrics
        metrics_tracker.increment("source_ops_count")
        metrics_tracker.calculate_lag(self.source_sequence_no, sync_state)
        
        # Track as most recently modified row
        self.last_modified_row_id = row_id
        
        return row_id
    
    def update_with_null(self, metrics_tracker, sync_state):
        """Update a row with a NULL score"""
        return self.update_row(metrics_tracker, sync_state, score=None)
    
    def delete_row(self, metrics_tracker, sync_state):
        """Delete a row from the source database"""
        if not self.source_db:
            logging.info("No rows to delete")
            return None
            
        row_id = random.choice(list(self.source_db.keys()))
        self.source_db_log.append({
            'ts': self.source_sequence_no,
            'id': row_id,
            'deleted': True,
        })
        
        # Update materialized view
        self.source_db.pop(row_id, None)
        
        logging.debug(f"DELETE: id={row_id}, ts={self.source_sequence_no}")
        self.source_sequence_no += 1
        
        # Update metrics
        metrics_tracker.increment("source_ops_count")
        metrics_tracker.set("source_db_size", len(self.source_db))
        metrics_tracker.calculate_lag(self.source_sequence_no, sync_state)
        
        # Since this row is deleted, it's not the most recently modified
        self.last_modified_row_id = None
        
        return row_id
    
    def find(self, id):
        """Find all operations for a specific ID"""
        return [row for row in self.source_db_log if row['id'] == id]
    
    def random_operation(self, metrics_tracker, sync_state):
        """Perform a random operation (insert, update, or delete)"""
        if random.random() < 0.3:
            return self.insert_row(metrics_tracker, sync_state)
        elif random.random() < 0.5:
            return self.update_row(metrics_tracker, sync_state)
        else:
            return self.delete_row(metrics_tracker, sync_state)
    
    def corrupt_target_score(self, metrics_tracker):
        """Directly corrupt a random row in the target database"""
        # Safety check - make sure target DB has rows
        if len(self.target_db) == 0:
            logging.info("No rows in target database to corrupt")
            return False
        
        # Choose a random row ID from target DB
        row_id = random.choice(list(self.target_db.keys()))
        target_row = self.target_db[row_id]
        
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
        self.target_db[row_id] = target_row
        
        # Increment corruption count
        metrics_tracker.increment("corruption_count")
        
        logging.info(f"TARGET CORRUPTED: id={row_id}, old_score={original_score}, new_score={new_score}")
        
        return row_id

class Corruptor:
    """Class responsible for corruption-related operations"""
    
    def __init__(self):
        self.corrupt_filter = set()  # IDs that should be filtered out of the pipeline
        self.corrupt_nulls = False   # Whether NULL values should be replaced with 0.0
        
    def corrupt_by_update(self, database, metrics_tracker, sync_state):
        """Update a row in the source and add its ID to the corrupt filter"""
        # Safety check
        if len(database.source_db) == 0:
            logging.info("No rows to update in corrupt_by_update")
            return False
            
        # Choose a row
        row_id = random.choice(list(database.source_db.keys()))
        original_row = database.source_db[row_id]
        
        # Create updated row
        new_row = original_row.copy()
        new_row['ts'] = database.source_sequence_no
        new_row['score'] = round(random.random() * 100, 2)
        
        # Add to operation log
        database.source_db_log.append(new_row)
        
        # Update materialized view
        database.source_db[row_id] = new_row
        
        # Update metadata
        database.source_sequence_no += 1
        metrics_tracker.increment("source_ops_count")
        metrics_tracker.calculate_lag(database.source_sequence_no, sync_state)
        
        # Add the row ID to the corrupt filter
        self.corrupt_filter.add(row_id)
        
        # Increment corruption count
        metrics_tracker.increment("corruption_count")
        
        # Track as most recently modified row
        database.last_modified_row_id = row_id
        
        logging.info(f"UPDATE: id={row_id}, old_score={original_row['score']}, new_score={new_row['score']}")
        logging.info(f"CORRUPT FILTER: Added id={row_id} after update")
        
        return row_id
    
    def corrupt_by_insert(self, database, metrics_tracker, sync_state):
        """Insert a new row in the source and add its ID to the corrupt filter"""
        # Create a new row
        row_id = database.primary_key_sequence_no
        new_row = {
            'ts': database.source_sequence_no,
            'id': row_id,
            'deleted': False,
            'name': database.fake.name(),
            'score': round(random.random() * 100, 2),
        }
        
        # Add to operation log
        database.source_db_log.append(new_row)
        
        # Update materialized view
        database.source_db[row_id] = new_row
        
        # Update metadata
        database.source_sequence_no += 1
        database.primary_key_sequence_no += 1
        metrics_tracker.increment("source_ops_count")
        metrics_tracker.set("source_db_size", len(database.source_db))
        metrics_tracker.calculate_lag(database.source_sequence_no, sync_state)
        
        # Add the row ID to the corrupt filter
        self.corrupt_filter.add(row_id)
        
        # Increment corruption count
        metrics_tracker.increment("corruption_count")
        
        # Track as most recently modified row
        database.last_modified_row_id = row_id
        
        logging.info(f"INSERT: id={row_id}, name={new_row['name']}, score={new_row['score']}")
        logging.info(f"CORRUPT FILTER: Added id={row_id} after insert")
        
        return row_id
    
    def remove_from_filter(self):
        """Remove a random ID from the corrupt filter"""
        if not self.corrupt_filter:
            logging.info("No IDs in corrupt filter to remove")
            return False
            
        # Pick a random ID to remove
        row_id = random.choice(list(self.corrupt_filter))
        self.corrupt_filter.remove(row_id)
        logging.info(f"CORRUPT FILTER: Removed id={row_id}")
        return row_id
    
    def toggle_null_corruption(self):
        """Toggle whether NULL values should be corrupted to 0.0 during loading"""
        # Toggle the corrupt_nulls flag
        self.corrupt_nulls = not self.corrupt_nulls
        
        if self.corrupt_nulls:
            logging.info("NULL CORRUPTION: Enabled (NULL Mismap)")
        else:
            logging.info("NULL CORRUPTION: Disabled")
        
        return self.corrupt_nulls

class ETLProcessor:
    """Class responsible for ETL (Extract-Transform-Load) operations"""
    
    def __init__(self):
        self.staging = []
        self.sync_state = {
            'last_extract_ts': -1,
            'last_load_ts': -1,
        }
        self.tracing_ids = []
    
    def extract(self, database, up_to_ts=None):
        """Extract data from source database"""
        # If no timestamp is provided, use the current sequence number
        if up_to_ts is None:
            up_to_ts = database.source_sequence_no
            
        logging.debug(f"EXTRACT: from_ts={self.sync_state['last_extract_ts']}, to_ts={up_to_ts}")
        
        incremental = [
            row for row in database.source_db_log
            if row['ts'] > self.sync_state['last_extract_ts']
        ]
        self.staging.extend(incremental)
        logging.debug(f"EXTRACT: from_ts={self.sync_state['last_extract_ts']}, to_ts={up_to_ts}, rows_extracted={len(incremental)}")
        self.sync_state['last_extract_ts'] = up_to_ts
        database.source_sequence_no += 1
        
        for row in self.staging:
            if row['id'] in self.tracing_ids:
                logging.info(f"[TRACE] EXTRACT: id={row['id']}, ts={row['ts']}, deleted={row['deleted']}")
                
        return len(incremental)
    
    def load(self, database, corruptor, metrics_tracker):
        """Load data into target database"""
        logging.debug(f"LOAD: processing {len(self.staging)} rows")
        
        if not self.staging:
            logging.info("No data to load")
            return 0
            
        # Process all operations in staging
        deletes = set()
        count = 0
        filtered_count = 0
        null_corrupted_count = 0
        
        for row in self.staging:
            row_id = row['id']
            
            # Check if the ID is in the corrupt filter
            if row_id in corruptor.corrupt_filter:
                filtered_count += 1
                logging.info(f"FILTERED: id={row_id} (blocked by corrupt filter)")
                continue
                
            count += 1
            
            if row.get('deleted', False):
                deletes.add(row_id)
                database.target_db.pop(row_id, None)
                
                if row_id in self.tracing_ids:
                    logging.info(f"[TRACE] LOAD - DELETE: id={row_id}, ts={row['ts']}, deleted={row['deleted']}")
            else:
                deletes.discard(row_id)
                # Create a copy without the 'deleted' field for target
                target_row = row.copy()
                if 'deleted' in target_row:
                    target_row.pop('deleted')
                if 'ts' in target_row:
                    target_row.pop('ts')
                    
                # Check for NULL values in the score field and corrupt if necessary
                if corruptor.corrupt_nulls and target_row.get('score') is None:
                    target_row['score'] = 0.0  # Replace NULL with 0.0
                    null_corrupted_count += 1
                    logging.info(f"NULL CORRUPTED: id={row_id} (NULL Mismap)")
                    
                database.target_db[row_id] = target_row
                
                if row_id in self.tracing_ids:
                    logging.info(f"[TRACE] LOAD - UPSERT: id={row_id}, ts={row['ts']}, deleted={row['deleted']}")
                    
        # Update sync state to mark when the last load occurred
        self.sync_state['last_load_ts'] = database.source_sequence_no
        
        # Update metrics
        metrics_tracker.increment("target_ops_count", count)
        metrics_tracker.set("target_db_size", len(database.target_db))
        if null_corrupted_count > 0:
            metrics_tracker.increment("corruption_count", null_corrupted_count)
            
        # Calculate lag
        metrics_tracker.calculate_lag(database.source_sequence_no, self.sync_state)
        
        # Clear staging area
        self.staging = []
        
        # Log the complete results
        status_parts = []
        if count > 0:
            status_parts.append(f"{count} operations loaded")
        if filtered_count > 0:
            status_parts.append(f"{filtered_count} filtered out")
        if null_corrupted_count > 0:
            status_parts.append(f"{null_corrupted_count} NULL values mismapped")
            
        logging.info(f"LOAD: {', '.join(status_parts)}")
        
        return count
    
    def trace(self, id, database):
        """Enable tracing for a specific ID"""
        if id not in self.tracing_ids:
            self.tracing_ids.append(id)
            logging.info(f"[TRACE] Added tracing for id={id}")
        
        logging.info(f"[TRACE] source_db={database.find(id)}")
        logging.info(f"[TRACE] target_db={database.target_db.get(id, None)}")
        return id

class ValidationEngine:
    """Class responsible for data validation"""
    
    def __init__(self):
        self.seatbelt = {}
    
    def seatbelt_check(self, database, etl_processor, metrics_tracker):
        """Validate data between source and target databases"""
        source_db_signatures = {
            row['id']: row['ts'] for row in database.source_db.values()
        }
        target_db_signatures = {
            k: hashlib.sha256(json.dumps(v, sort_keys=True).encode()).hexdigest() 
            for k, v in database.target_db.items()
        }
        
        ids = set(source_db_signatures.keys()) | \
            set(target_db_signatures.keys()) | \
            set(self.seatbelt.keys())
            
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
            seatbelt_row = self.seatbelt.get(id, {})
            
            source_operation = determine_source_operation(source_signature, seatbelt_row.get('source_signature', None))
            target_operation = determine_source_operation(target_signature, seatbelt_row.get('target_signature', None))
            previous_source_operation = seatbelt_row.get('source_operation', None)
            previous_target_operation = seatbelt_row.get('target_operation', None)
            previous_error = seatbelt_row.get('validation_error', False)
            
            # Check NULL equivalence between source and target rows
            null_mismatch = False
            if source_signature is not None and target_signature is not None:
                source_row = database.source_db.get(id, {})
                target_row = database.target_db.get(id, {})
                
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
                
            if id in etl_processor.tracing_ids:
                logging.info(f"[TRACE] SEATBELT CHECK: id={id}, source_operation={source_operation}, previous_source_operation={previous_source_operation}, target_operation={target_operation}, previous_target_operation={previous_target_operation}, previous_error={previous_error}, error={error}, null_mismatch={null_mismatch}")
                
            self.seatbelt[id] = {
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
                    
                logging.error(f"Validation error for id={id} persists")
                logging.error(f"seatbelt error: id={id}, source_operation={source_operation}, previous_source_operation={previous_source_operation}, target_operation={target_operation}, previous_target_operation={previous_target_operation}, previous_error={previous_error}, error={error}, null_mismatch={null_mismatch}")
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
        metrics_tracker.update(
            source_db_size=len(database.source_db),
            target_db_size=len(database.target_db),
            seatbelt_size=len(self.seatbelt),
            error_count=error_count,
            pending_count=pending_count,
            valid_count=valid_count,
        )
        
        # Display categorized errors
        if error_count > 0:
            if source_only_ids:
                source_only_str = ", ".join(str(id) for id in source_only_ids[:5])
                if len(source_only_ids) > 5:
                    source_only_str += f" (and {len(source_only_ids) - 5} more)"
                logging.error(f"Source-Only Rows: {source_only_str}")
                
            if target_only_ids:
                target_only_str = ", ".join(str(id) for id in target_only_ids[:5])
                if len(target_only_ids) > 5:
                    target_only_str += f" (and {len(target_only_ids) - 5} more)"
                logging.error(f"Target-Only Rows: {target_only_str}")
                
            if stale_ids:
                stale_str = ", ".join(str(id) for id in stale_ids[:5])
                if len(stale_ids) > 5:
                    stale_str += f" (and {len(stale_ids) - 5} more)"
                logging.error(f"Drifted Rows: {stale_str}")
        
        logging.info(f"SEATBELT CHECK: valid={valid_count}, in-flight={pending_count}, discrepant={error_count}")
        logging.info(f"Metrics: {metrics_tracker.get()}")
        
        return metrics_tracker.get()

class Simulator:
    """Main class for orchestrating data simulation and validation"""
    
    def __init__(self, random_seed=42):
        # Initialize components
        self.metrics_tracker = MetricsTracker()
        self.database = Database(random_seed)
        self.corruptor = Corruptor()
        self.etl_processor = ETLProcessor()
        self.validation_engine = ValidationEngine()
        
        # Set up custom logger
        self.logger = logging.getLogger(__name__)
        
        # Seatbelt configuration
        self.seatbelt_interval = 25
        self.last_seatbelt_check_ts = 0
        self.seatbelt_check_sleep = 0
        
        # Create timestamp adapter
        self.timestamp_adapter = self._create_timestamp_adapter()
    
    def _create_timestamp_adapter(self):
        """Create a custom logging adapter that includes the current timestamp"""
        class TimestampAdapter(logging.LoggerAdapter):
            def process(self, msg, kwargs):
                return f"current_ts={self.database.source_sequence_no} - {msg}", kwargs
        
        return TimestampAdapter(self.logger, {})
    
    def insert_row(self):
        """Insert a row into the source database"""
        return self.database.insert_row(self.metrics_tracker, self.etl_processor.sync_state)
    
    def insert_with_null(self):
        """Insert a row with NULL score into the source database"""
        return self.database.insert_with_null(self.metrics_tracker, self.etl_processor.sync_state)
    
    def update_row(self):
        """Update a row in the source database"""
        return self.database.update_row(self.metrics_tracker, self.etl_processor.sync_state)
    
    def update_with_null(self):
        """Update a row with NULL score in the source database"""
        return self.database.update_with_null(self.metrics_tracker, self.etl_processor.sync_state)
    
    def delete_row(self):
        """Delete a row from the source database"""
        return self.database.delete_row(self.metrics_tracker, self.etl_processor.sync_state)
    
    def corrupt_by_update(self):
        """Update a row and mark it as corrupted"""
        return self.corruptor.corrupt_by_update(self.database, self.metrics_tracker, self.etl_processor.sync_state)
    
    def corrupt_by_insert(self):
        """Insert a row and mark it as corrupted"""
        return self.corruptor.corrupt_by_insert(self.database, self.metrics_tracker, self.etl_processor.sync_state)
    
    def corrupt_target_score(self):
        """Directly corrupt a score in the target database"""
        return self.database.corrupt_target_score(self.metrics_tracker)
    
    def remove_from_filter(self):
        """Remove an ID from the corruption filter"""
        return self.corruptor.remove_from_filter()
    
    def toggle_null_corruption(self):
        """Toggle whether NULL values should be corrupted during loading"""
        return self.corruptor.toggle_null_corruption()
    
    def extract(self, up_to_ts=None):
        """Extract data from the source database"""
        return self.etl_processor.extract(self.database, up_to_ts)
    
    def load(self):
        """Load data into the target database"""
        return self.etl_processor.load(self.database, self.corruptor, self.metrics_tracker)
    
    def random_operation(self):
        """Perform a random operation on the source database"""
        return self.database.random_operation(self.metrics_tracker, self.etl_processor.sync_state)
    
    def seatbelt_check(self):
        """Perform validation between source and target databases"""
        return self.validation_engine.seatbelt_check(self.database, self.etl_processor, self.metrics_tracker)
    
    def find(self, id):
        """Find all operations for a specific ID"""
        return self.database.find(id)
    
    def trace(self, id):
        """Enable tracing for a specific ID"""
        return self.etl_processor.trace(id, self.database)
    
    def run_simulation(self):
        """Run a simulation with a predefined plan"""
        # Create Plan
        plan = []
        
        # Initial inserts with tracing
        for i in range(10):
            plan.append(lambda: self.trace(self.insert_row()))
        plan.append(lambda: self.extract(self.database.source_sequence_no))
        plan.append(lambda: self.load())
        
        # Add examples of various operations
        plan.append(lambda: self.insert_with_null())
        plan.append(lambda: self.update_with_null())
        plan.append(lambda: self.corrupt_by_insert())
        plan.append(lambda: self.corrupt_by_update())
        plan.append(lambda: self.toggle_null_corruption())
        plan.append(lambda: self.extract())
        plan.append(lambda: self.load())
        plan.append(lambda: self.corrupt_target_score())
        plan.append(lambda: self.seatbelt_check())
        plan.append(lambda: self.remove_from_filter())
        
        # Regular operation cycle
        for i in range(20):
            for j in range(4):
                plan.append(lambda: self.trace(self.random_operation()))
            plan.append(lambda: self.extract(self.database.source_sequence_no))
            for j in range(2):
                plan.append(lambda: self.trace(self.random_operation()))
            plan.append(lambda: self.load())
            for j in range(5):
                plan.append(lambda: self.trace(self.random_operation()))
                
        # Execute Plan
        for step in plan:
            step()
            
            if (self.database.source_sequence_no - self.last_seatbelt_check_ts > 
                    (self.seatbelt_interval + self.seatbelt_check_sleep)):
                if self.etl_processor.sync_state['last_load_ts'] > self.last_seatbelt_check_ts:
                    self.seatbelt_check()
                    self.last_seatbelt_check_ts = self.database.source_sequence_no
                    self.seatbelt_check_sleep = 0
                else:
                    logging.info(f"Waiting for load to complete before next seatbelt check")
                    self.seatbelt_check_sleep += 10

if __name__ == '__main__':
    # Create and run the simulator
    simulator = Simulator()
    simulator.run_simulation() 