"""Database operations for the Seatbelt Demo simulator."""

from faker import Faker
import random
import logging
from datetime import datetime, date
from typing import Dict, List, Any, Optional, Tuple, Union, Callable
from dataclasses import dataclass, field
from .column_types import ColumnType

@dataclass
class ColumnDefinition:
    """Definition of a database column"""
    name: str
    type: ColumnType
    nullable: bool = False
    
    # Optional type mapping for target database (e.g., INTEGER -> FLOAT)
    target_type: Optional[ColumnType] = None
    
    # Function to generate test data for this column
    generator: Optional[Callable] = None
    
    # Whether this column should be synced to the target
    sync_to_target: bool = True
    
    # Whether this column exists only in the target
    target_only: bool = False
    
    # Configuration for computed columns
    computed_from: Optional[Dict[str, Any]] = None

@dataclass
class SchemaDefinition:
    """Schema definition for a database table"""
    columns: List[ColumnDefinition] = field(default_factory=list)
    # Always has an 'id' column by default
    
    def add_column(self, column: ColumnDefinition) -> None:
        """Add a column to the schema"""
        self.columns.append(column)
    
    def get_column_by_name(self, name: str) -> Optional[ColumnDefinition]:
        """Get a column by name"""
        for column in self.columns:
            if column.name == name:
                return column
        return None
    
    def has_column(self, name: str) -> bool:
        """Check if schema has a column with the given name"""
        return any(column.name == name for column in self.columns)

    def iter_source_columns(self, include_id: bool = False):
        """Iterate over columns that exist in the *source* database (i.e. not target_only)."""
        for column in self.columns:
            if column.target_only:
                # Columns that exist only in the target DB should be skipped in source operations
                continue
            if not include_id and column.name == 'id':
                continue
            yield column

    def iter_target_columns(self, include_id: bool = False):
        """Iterate over columns that should exist in the *target* database.

        A column appears in the target if it is either explicitly marked as ``target_only`` or
        it is a regular column that is allowed to be synced to the target (``sync_to_target``).
        """
        for column in self.columns:
            if column.name == 'id' and not include_id:
                continue
            if column.target_only or column.sync_to_target:
                yield column

@dataclass
class InitialData:
    """Initial data configuration for the database"""
    row_count: int = 3
    rows: List[Dict[str, Any]] = field(default_factory=list)
    
    def add_row(self, row: Dict[str, Any]) -> None:
        """Add a row to the initial data"""
        self.rows.append(row)

class Database:
    """Class responsible for database operations"""
    
    def __init__(self, random_seed=42, schema=None, initial_data=None):
        # Set seeds for reproducibility
        self.random_seed = random_seed
        random.seed(self.random_seed)
        self.fake = Faker()
        self.fake.seed_instance(self.random_seed)
        
        # Initialize schema
        self.schema = schema or self._create_default_schema()
        
        # Add ID column if not present
        if not self.schema.has_column('id'):
            id_column = ColumnDefinition(
                name='id',
                type=ColumnType.INTEGER,
                nullable=False
            )
            self.schema.columns.insert(0, id_column)
        
        # Initialize database structures
        self.source_db_log = []
        self.source_db = {}  # Materialized view of source
        self.target_db = {}
        
        # Initialize sequence counters
        self.source_sequence_no = 0
        self.primary_key_sequence_no = 1
        self.last_modified_row_id = None
        
        # Initialize with provided data or generate default
        if initial_data:
            self._initialize_with_data(initial_data)
        
    def _create_default_schema(self) -> SchemaDefinition:
        """Create a default schema with name and score columns"""
        schema = SchemaDefinition()
        
        # Add name column
        name_column = ColumnDefinition(
            name='name',
            type=ColumnType.STRING,
            nullable=False,
            generator=lambda: self.fake.name()
        )
        schema.add_column(name_column)
        
        # Add score column
        score_column = ColumnDefinition(
            name='score',
            type=ColumnType.FLOAT,
            nullable=True,
            generator=lambda: round(random.random() * 100, 2)
        )
        schema.add_column(score_column)
        
        return schema
    
    def _initialize_with_data(self, initial_data: InitialData) -> None:
        """Initialize the database with the provided data"""
        if initial_data.rows:
            # Use explicitly provided rows
            for row_data in initial_data.rows:
                self._create_row_from_data(row_data)
        else:
            # Generate random data based on schema and row count
            for _ in range(initial_data.row_count):
                self._create_random_row()
    
    def _create_row_from_data(self, row_data: Dict[str, Any]) -> int:
        """Create a row from the provided data"""
        new_row = {
            'ts': self.source_sequence_no,
            'id': row_data.get('id', self.primary_key_sequence_no),
            'deleted': False
        }
        
        # Add data for each column in the schema (source columns only)
        for column in self.schema.iter_source_columns():
            # Use provided value or generate one
            if column.name in row_data:
                new_row[column.name] = row_data[column.name]
            elif column.generator:
                new_row[column.name] = column.generator()
            elif not column.nullable:
                # Generate default value based on type
                new_row[column.name] = self._generate_default_value(column.type)
        
        # Update database
        self.source_db_log.append(new_row)
        self.source_db[new_row['id']] = new_row
        self.source_sequence_no += 1
        
        # Update primary key sequence if needed
        if new_row['id'] >= self.primary_key_sequence_no:
            self.primary_key_sequence_no = new_row['id'] + 1
        
        return new_row['id']
    
    def _create_random_row(self) -> int:
        """Create a row with random data based on the schema"""
        new_row = {
            'ts': self.source_sequence_no,
            'id': self.primary_key_sequence_no,
            'deleted': False
        }
        
        # Generate data for each source column in the schema
        for column in self.schema.iter_source_columns():
            if column.generator:
                # Use custom generator
                new_row[column.name] = column.generator()
            else:
                # Generate based on type
                new_row[column.name] = self._generate_value_for_type(column.type, column.nullable)
        
        # Update database
        self.source_db_log.append(new_row)
        self.source_db[self.primary_key_sequence_no] = new_row
        self.source_sequence_no += 1
        self.primary_key_sequence_no += 1
        
        return new_row['id']
    
    def _generate_value_for_type(self, column_type: ColumnType, nullable: bool) -> Any:
        """Generate a random value based on the column type"""
        # Potentially return NULL value
        if nullable and random.random() < 0.1:  # 10% chance of NULL
            return None
            
        return self._generate_default_value(column_type)
    
    def _generate_default_value(self, column_type: ColumnType) -> Any:
        """Generate a default value for a column type"""
        if column_type == ColumnType.INTEGER:
            return random.randint(1, 1000)
        elif column_type == ColumnType.INTEGER32:
            # Generate an integer within int32 bounds
            return random.randint(-2147483648, 2147483647)
        elif column_type == ColumnType.FLOAT:
            return round(random.random() * 100, 2)
        elif column_type == ColumnType.FLOAT32:
            # Generate a float and format as float32 string
            return f"{round(random.random() * 100, 2):.7g}"
        elif column_type == ColumnType.DECIMAL:
            # For DECIMAL(10,2), generate values with exactly 2 decimal places
            return round(random.random() * 100000000, 2)
        elif column_type == ColumnType.STRING:
            return self.fake.word()
        elif column_type == ColumnType.BOOLEAN:
            return random.choice([True, False])
        elif column_type == ColumnType.DATE:
            return self.fake.date_object()
        elif column_type == ColumnType.DATETIME:
            return self.fake.date_time()
        elif column_type == ColumnType.JSON:
            # Generate a simple JSON object or array
            json_types = [
                {},  # empty object
                [],  # empty array
                {"a": 1, "b": 2},  # simple object
                [1, 2, 3],  # simple array
                {"nested": {"x": 1, "y": 2}, "list": [1, 2, 3]}  # nested structure
            ]
            return random.choice(json_types)
        else:
            return None
    
    def insert_row(self, metrics_tracker, sync_state, custom_values=None):
        """Insert a new row into the source database with optional custom values"""
        # Print stack trace of the caller
        new_row = {
            'ts': self.source_sequence_no,
            'id': self.primary_key_sequence_no,
            'deleted': False,
        }
        
        # Generate data for each source column in the schema
        for column in self.schema.iter_source_columns():
            # Use custom value if provided
            if custom_values and column.name in custom_values:
                new_row[column.name] = custom_values[column.name]
            elif column.generator:
                # Use custom generator
                new_row[column.name] = column.generator()
            else:
                # Generate based on type
                new_row[column.name] = self._generate_value_for_type(column.type, column.nullable)
        
        self.source_db_log.append(new_row)
        
        # Update materialized view
        self.source_db[self.primary_key_sequence_no] = new_row
        
        # Create log message
        log_parts = [f"INSERT: id={self.primary_key_sequence_no}, ts={self.source_sequence_no}"]
        for column in self.schema.iter_source_columns():
            if column.name in new_row:
                log_parts.append(f"{column.name}={new_row[column.name]}")
        
        logging.info(", ".join(log_parts))
        
        self.source_sequence_no += 1
        self.primary_key_sequence_no += 1
        
        # Update metrics
        metrics_tracker.increment("source_ops_count")
        metrics_tracker.set("source_db_size", len(self.source_db))
        metrics_tracker.calculate_lag(self.source_sequence_no, sync_state)
        
        # Track as most recently modified row
        self.last_modified_row_id = new_row['id']
        
        return new_row['id']
    
    def insert_with_null(self, metrics_tracker, sync_state, null_column=None):
        """Insert a new row with a NULL value in the specified column"""
        # Find a nullable column if not specified
        if null_column is None:
            nullable_columns = [col for col in self.schema.columns 
                               if col.nullable and col.name != 'id']
            if not nullable_columns:
                logging.info("No nullable columns to insert NULL value")
                return None
            null_column = random.choice(nullable_columns).name
        
        # Create custom values dict with NULL for the specified column
        custom_values = {null_column: None}
        return self.insert_row(metrics_tracker, sync_state, custom_values)
    
    def update_row(self, metrics_tracker, sync_state, row_id=None, custom_values=None):
        """Update an existing row in the source database"""
        if not self.source_db:
            logging.info("No rows to update")
            return None
            
        # Select a row to update
        if row_id is None or row_id not in self.source_db:
            row_id = random.choice(list(self.source_db.keys()))
            
        original_row = self.source_db[row_id]
        new_row = original_row.copy()
        new_row['ts'] = self.source_sequence_no
        
        # Update fields
        for column in self.schema.iter_source_columns():
            if random.random() < 0.5:  # 50% chance to update each field
                if column.generator:
                    new_row[column.name] = column.generator()
                else:
                    new_row[column.name] = self._generate_value_for_type(column.type, column.nullable)
        
        self.source_db_log.append(new_row)
        
        # Update materialized view
        self.source_db[row_id] = new_row
        
        # Build log message with changes
        log_parts = [f"UPDATE: id={row_id}, ts={self.source_sequence_no}"]
        for column in self.schema.iter_source_columns():
            if (column.name in original_row and column.name in new_row and 
                original_row.get(column.name) != new_row.get(column.name)):
                log_parts.append(f"{column.name}: {original_row.get(column.name)} -> {new_row.get(column.name)}")
        
        logging.info(", ".join(log_parts))
        self.source_sequence_no += 1
        
        # Update metrics
        metrics_tracker.increment("source_ops_count")
        metrics_tracker.calculate_lag(self.source_sequence_no, sync_state)
        
        # Track as most recently modified row
        self.last_modified_row_id = row_id
        
        return row_id
    
    def update_with_null(self, metrics_tracker, sync_state, null_column=None):
        """Update a row with a NULL value in a specified column"""
        # Find a nullable column if not specified
        if null_column is None:
            nullable_columns = [col for col in self.schema.columns 
                               if col.nullable and col.name != 'id']
            if not nullable_columns:
                logging.info("No nullable columns to set to NULL")
                return None
            null_column = random.choice(nullable_columns).name
        
        # Create custom values dict with NULL for the specified column
        custom_values = {null_column: None}
        return self.update_row(metrics_tracker, sync_state, None, custom_values)
    
    def delete_row(self, metrics_tracker, sync_state, row_id=None):
        """Delete a row from the source database"""
        if not self.source_db:
            logging.info("No rows to delete")
            return None
            
        # Select a row to delete
        if row_id is None or row_id not in self.source_db:
            row_id = random.choice(list(self.source_db.keys()))
            
        self.source_db_log.append({
            'ts': self.source_sequence_no,
            'id': row_id,
            'deleted': True,
        })
        
        # Update materialized view
        self.source_db.pop(row_id, None)
        
        logging.info(f"DELETE: id={row_id}, ts={self.source_sequence_no}")
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
        """Directly corrupt a random column value in the target database"""
        # Safety check - make sure target DB has rows
        if len(self.target_db) == 0:
            logging.info("No rows in target database to corrupt")
            return False
        
        # Choose a random row ID from target DB
        row_id = random.choice(list(self.target_db.keys()))
        target_row = self.target_db[row_id]
        
        # Choose a random column that's not id to corrupt
        corruptible_columns = [col.name for col in self.schema.columns 
                              if col.name != 'id' and col.name in target_row]
        
        if not corruptible_columns:
            logging.info(f"No columns to corrupt in row id={row_id}")
            return False
            
        column_to_corrupt = random.choice(corruptible_columns)
        column_def = self.schema.get_column_by_name(column_to_corrupt)
        
        # Store the original value for logging
        original_value = target_row.get(column_to_corrupt)
        
        # Generate a new, different value
        if original_value is None:
            # Change NULL to a non-NULL value
            new_value = self._generate_default_value(column_def.type)
        else:
            # Ensure the new value is different from the original
            while True:
                new_value = self._generate_value_for_type(column_def.type, False)  # Force non-NULL
                # For simple types, check direct inequality
                if new_value != original_value:
                    break
                # For complex types like dates, might need additional checks
        
        # Update the row with the corrupted value
        target_row[column_to_corrupt] = new_value
        self.target_db[row_id] = target_row
        
        # Increment corruption count
        metrics_tracker.increment("corruption_count")
        
        logging.info(f"TARGET CORRUPTED: id={row_id}, column={column_to_corrupt}, old_value={original_value}, new_value={new_value}")
        
        return row_id
        
    def corrupt_target_with_row(self, metrics_tracker, row_id, row_data):
        """Directly corrupt a target row with specified data
        
        Args:
            metrics_tracker: The metrics tracker to update
            row_id: ID of the row to corrupt
            row_data: Dictionary of column values to set in the target row
        """
        # Check if the row exists in the target database
        if row_id not in self.target_db:
            logging.warning(f"Row ID {row_id} not found in target database")
            return False
            
        target_row = self.target_db[row_id]
        
        # Keep track of changed columns for logging
        changed_columns = []
        
        # Update the row with the provided values
        for column_name, new_value in row_data.items():
            # Skip 'id' column as it's the primary key
            if column_name == 'id':
                continue
                
            # Check if the column exists in schema
            column_def = self.schema.get_column_by_name(column_name)
            if not column_def:
                logging.warning(f"Column {column_name} not found in schema")
                continue
                
            # Record the original value for logging
            original_value = target_row.get(column_name)
            
            # Update the value
            target_row[column_name] = new_value
            
            # Track the change
            changed_columns.append((column_name, original_value, new_value))
        
        # Update the row in the target database
        self.target_db[row_id] = target_row
        
        # Increment corruption count
        metrics_tracker.increment("corruption_count")
        
        # Log the changes
        if changed_columns:
            log_parts = [f"TARGET CORRUPTED: id={row_id}"]
            for col_name, old_val, new_val in changed_columns:
                log_parts.append(f"{col_name}: {old_val} -> {new_val}")
            logging.info(", ".join(log_parts))
            
            return row_id
        else:
            logging.info(f"No changes made to row id={row_id}")
            return False 