"""Corruption operations for the Seatbelt Demo simulator."""

import random
import logging
from typing import Dict, Any, List, Optional, Set

class Corruptor:
    """Class responsible for corruption-related operations"""
    
    def __init__(self):
        self.corrupt_filter = set()  # IDs that should be filtered out of the pipeline
        self.corrupt_nulls = False   # Whether NULL values should be corrupted
    
    def corrupt_by_update(self, database, metrics_tracker, sync_state, row=None):
        """Update a row in the source and add its ID to the corrupt filter
        
        Args:
            database: The database instance
            metrics_tracker: The metrics tracker instance
            sync_state: The current sync state
            row: Optional dictionary with row values including id to update
        """
        # Safety check
        if len(database.source_db) == 0:
            logging.info("No rows to update in corrupt_by_update")
            return False
            
        # Choose a row
        if row and 'id' in row:
            row_id = row['id']
            if row_id not in database.source_db:
                logging.info(f"Row id={row_id} not found in source_db for corrupt_by_update")
                return False
        else:
            row_id = random.choice(list(database.source_db.keys()))
            
        original_row = database.source_db[row_id]
        
        # Create updated row
        new_row = original_row.copy()
        new_row['ts'] = database.source_sequence_no
        
        # If row is provided, use those values
        if row:
            for key, value in row.items():
                if key != 'id' and key != 'ts':  # Don't override id or ts
                    new_row[key] = value
            updated_columns = [(key, original_row.get(key), value) for key, value in row.items() 
                              if key != 'id' and key != 'ts' and original_row.get(key) != value]
        else:
            # Update one or more columns randomly
            updated_columns = []
            
            for column in database.schema.columns:
                if column.name == 'id':
                    continue  # Don't update ID
                
                # 50% chance to update each column
                if random.random() < 0.5:
                    old_value = original_row.get(column.name)
                    if column.generator:
                        new_value = column.generator()
                    else:
                        new_value = database._generate_value_for_type(column.type, column.nullable)
                    
                    new_row[column.name] = new_value
                    updated_columns.append((column.name, old_value, new_value))
            
            # Ensure at least one column is updated
            if not updated_columns:
                # Choose a random column to update
                updateable_columns = [col for col in database.schema.columns if col.name != 'id']
                if updateable_columns:
                    column = random.choice(updateable_columns)
                    old_value = original_row.get(column.name)
                    if column.generator:
                        new_value = column.generator()
                    else:
                        new_value = database._generate_value_for_type(column.type, column.nullable)
                    
                    new_row[column.name] = new_value
                    updated_columns.append((column.name, old_value, new_value))
        
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
        
        # Create log message
        log_parts = [f"UPDATE: id={row_id}, ts={database.source_sequence_no}"]
        for col_name, old_val, new_val in updated_columns:
            log_parts.append(f"{col_name}: {old_val} -> {new_val}")
        
        logging.info(", ".join(log_parts))
        logging.info(f"CORRUPT FILTER: Added id={row_id} after update")
        
        return row_id
    
    def corrupt_by_insert(self, database, metrics_tracker, sync_state):
        """Insert a new row in the source and add its ID to the corrupt filter"""
        # Create a new row with ID and timestamp
        row_id = database.primary_key_sequence_no
        new_row = {
            'ts': database.source_sequence_no,
            'id': row_id,
            'deleted': False,
        }
        
        # Generate data for each column in the schema
        for column in database.schema.columns:
            if column.name == 'id':
                continue  # Already handled
                
            if column.generator:
                # Use custom generator
                new_row[column.name] = column.generator()
            else:
                # Generate based on type
                new_row[column.name] = database._generate_value_for_type(column.type, column.nullable)
        
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
        
        # Build log message
        log_parts = [f"INSERT: id={row_id}, ts={database.source_sequence_no}"]
        for column in database.schema.columns:
            if column.name != 'id':
                log_parts.append(f"{column.name}={new_row.get(column.name)}")
        
        logging.info(", ".join(log_parts))
        logging.info(f"CORRUPT FILTER: Added id={row_id} after insert")
        
        return row_id
    
    def corrupt_by_delete(self, database, metrics_tracker, sync_state):
        """Delete a row and add its ID to the corrupt filter to prevent loading"""
        # Safety check
        if len(database.source_db) == 0:
            logging.info("No rows to delete in corrupt_by_delete")
            return False
            
        # Choose a row to delete
        row_id = random.choice(list(database.source_db.keys()))
        
        # Add to operation log with deleted flag set to True
        deleted_row = database.source_db[row_id].copy()
        deleted_row['ts'] = database.source_sequence_no
        deleted_row['deleted'] = True
        database.source_db_log.append(deleted_row)
        
        # Remove from materialized view
        del database.source_db[row_id]
        
        # Update metadata
        database.source_sequence_no += 1
        metrics_tracker.increment("source_ops_count")
        metrics_tracker.set("source_db_size", len(database.source_db))
        metrics_tracker.calculate_lag(database.source_sequence_no, sync_state)
        
        # Add the row ID to the corrupt filter
        self.corrupt_filter.add(row_id)
        
        # Increment corruption count
        metrics_tracker.increment("corruption_count")
        
        # Track as most recently modified row
        database.last_modified_row_id = row_id
        
        # Create log message
        logging.info(f"DELETE: id={row_id}, ts={database.source_sequence_no}")
        logging.info(f"CORRUPT FILTER: Added id={row_id} after delete")
        
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
    
    def toggle_null_corruption(self, etl_processor=None, column_name=None):
        """Toggle whether NULL values should be corrupted during loading
        
        Args:
            etl_processor: Optional ETLProcessor instance to configure corruptible columns
            column_name: Optional specific column to toggle for NULL corruption
        """
        if column_name and etl_processor:
            # Toggle for specific column
            if column_name in etl_processor.null_corruptible_columns:
                etl_processor.null_corruptible_columns.remove(column_name)
                logging.info(f"NULL CORRUPTION: Disabled for column {column_name}")
                return False
            else:
                etl_processor.null_corruptible_columns.add(column_name)
                self.corrupt_nulls = True  # Enable global flag
                logging.info(f"NULL CORRUPTION: Enabled for column {column_name}")
                return True
        else:
            # Toggle global setting
            self.corrupt_nulls = not self.corrupt_nulls
            
            if self.corrupt_nulls:
                logging.info("NULL CORRUPTION: Enabled (NULL Mismap)")
            else:
                logging.info("NULL CORRUPTION: Disabled")
                
                # Clear corruptible columns if ETL processor provided
                if etl_processor:
                    etl_processor.clear_null_corruptible_columns()
            
            return self.corrupt_nulls
    
    def set_null_corruption_for_column(self, etl_processor, column_name, enabled=True):
        """Set NULL corruption for a specific column
        
        Args:
            etl_processor: ETLProcessor instance to configure
            column_name: Name of the column to configure
            enabled: True to enable NULL corruption, False to disable
        """
        if enabled:
            etl_processor.add_null_corruptible_column(column_name)
            self.corrupt_nulls = True
            logging.info(f"NULL CORRUPTION: Enabled for column {column_name}")
        else:
            etl_processor.null_corruptible_columns.discard(column_name)
            logging.info(f"NULL CORRUPTION: Disabled for column {column_name}")
            
            # If no columns are corruptible, disable global flag
            if not etl_processor.null_corruptible_columns:
                self.corrupt_nulls = False
                logging.info("NULL CORRUPTION: Disabled globally (no columns enabled)")
                
        return self.corrupt_nulls 