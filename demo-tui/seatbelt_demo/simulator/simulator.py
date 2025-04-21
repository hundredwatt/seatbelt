"""Main simulator class for Seatbelt Demo."""

import logging
import os
from typing import Callable, List, Optional, Dict, Any, Union
from pathlib import Path

from .metrics import MetricsTracker
from .database import Database, SchemaDefinition, InitialData, ColumnDefinition
from .corruptor import Corruptor
from .etl import ETLProcessor
from .validation import ValidationEngine
from .config import load_simulator_config, get_default_config, ConfigurationError
from .column_types import ColumnType

class Simulator:
    """Main class for orchestrating data simulation and validation"""
    
    def __init__(self, 
                 random_seed: int = 42, 
                 schema: Optional[SchemaDefinition] = None, 
                 initial_data: Optional[InitialData] = None,
                 config_file: Optional[Union[str, Path]] = None):
        """Initialize the simulator
        
        Args:
            random_seed: Random seed for reproducibility
            schema: Optional custom schema definition
            initial_data: Optional initial data configuration
            config_file: Optional path to a configuration file
        """
        # Load configuration if provided
        config = None
        if config_file:
            try:
                config = load_simulator_config(config_file)
                random_seed = config.get('random_seed', random_seed)
                schema = config.get('schema', schema)
                initial_data = config.get('initial_data', initial_data)
                self.seatbelt_interval = config.get('seatbelt_interval', 25)
            except ConfigurationError as e:
                logging.error(f"Error loading configuration: {e}")
                # Fallback to defaults
                config = get_default_config()
                initial_data = config.get('initial_data')
                self.seatbelt_interval = config.get('seatbelt_interval', 25)
        else:
            # Use default seatbelt interval if not from config
            self.seatbelt_interval = 25
            
        # Initialize components
        self.metrics_tracker = MetricsTracker()
        self.database = Database(random_seed, schema, initial_data)
        self.corruptor = Corruptor()
        self.etl_processor = ETLProcessor()
        self.validation_engine = ValidationEngine()
        
        # Set up custom logger
        self.logger = logging.getLogger(__name__)
        
        # Seatbelt configuration
        self.last_seatbelt_check_ts = 0
        self.seatbelt_check_sleep = 0
        
        # Create timestamp adapter
        self.timestamp_adapter = self._create_timestamp_adapter()
        
        # Simulation state
        self.simulation_running = False
        self.pause_simulation = False
        
        # Callbacks for UI
        self.on_data_changed = None
        
        # Set up default null corruptible columns
        self._setup_null_corruptible_columns()
    
    def _setup_null_corruptible_columns(self):
        """Set up which columns can have NULL values corrupted"""
        # Add all nullable columns to the corruptible list
        for column in self.database.schema.columns:
            if column.nullable and column.name != 'id':
                self.etl_processor.add_null_corruptible_column(column.name)
    
    def _create_timestamp_adapter(self):
        """Create a custom logging adapter that includes the current timestamp"""
        class TimestampAdapter(logging.LoggerAdapter):
            def process(self, msg, kwargs):
                return f"current_ts={self.extra['database'].source_sequence_no} - {msg}", kwargs
        
        return TimestampAdapter(self.logger, {'database': self.database})
    
    def insert_row(self, custom_values=None):
        """Insert a row into the source database with optional custom values"""
        result = self.database.insert_row(self.metrics_tracker, self.etl_processor.sync_state, custom_values)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def insert_with_null(self, null_column=None):
        """Insert a row with NULL value in a specified column"""
        result = self.database.insert_with_null(self.metrics_tracker, self.etl_processor.sync_state, null_column)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def update_row(self, row_id=None, custom_values=None):
        """Update a row in the source database"""
        result = self.database.update_row(self.metrics_tracker, self.etl_processor.sync_state, row_id, custom_values)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def update_with_null(self, null_column=None):
        """Update a row with NULL value in a specified column"""
        result = self.database.update_with_null(self.metrics_tracker, self.etl_processor.sync_state, null_column)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def delete_row(self, row_id=None):
        """Delete a row from the source database"""
        result = self.database.delete_row(self.metrics_tracker, self.etl_processor.sync_state, row_id)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def corrupt_by_update(self, row=None):
        """Update a row and mark it as corrupted
        
        Args:
            row: Optional dictionary with row values including id to update
        """
        result = self.corruptor.corrupt_by_update(self.database, self.metrics_tracker, self.etl_processor.sync_state, row)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def corrupt_by_insert(self):
        """Insert a row and mark it as corrupted"""
        result = self.corruptor.corrupt_by_insert(self.database, self.metrics_tracker, self.etl_processor.sync_state)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def corrupt_by_delete(self):
        """Delete a row and mark it as corrupted (won't load into target)"""
        result = self.corruptor.corrupt_by_delete(self.database, self.metrics_tracker, self.etl_processor.sync_state)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def corrupt_target_score(self):
        """Directly corrupt a column value in the target database"""
        result = self.database.corrupt_target_score(self.metrics_tracker)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def corrupt_target_with_row(self, row_id, row_data):
        """Directly corrupt a target row with specified data
        
        Args:
            row_id: ID of the row to corrupt
            row_data: Dictionary of column values to set in the target row
        """
        result = self.database.corrupt_target_with_row(self.metrics_tracker, row_id, row_data)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def remove_from_filter(self):
        """Remove an ID from the corruption filter"""
        result = self.corruptor.remove_from_filter()
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def toggle_null_corruption(self, column_name=None):
        """Toggle whether NULL values should be corrupted during loading
        
        Args:
            column_name: Optional column name to toggle NULL corruption specifically for this column
        """
        result = self.corruptor.toggle_null_corruption(self.etl_processor, column_name)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def set_null_corruption_for_column(self, column_name, enabled=True):
        """Set NULL corruption for a specific column
        
        Args:
            column_name: Name of the column to configure
            enabled: True to enable NULL corruption, False to disable
        """
        result = self.corruptor.set_null_corruption_for_column(self.etl_processor, column_name, enabled)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def extract(self, up_to_ts=None):
        """Extract data from the source database"""
        result = self.etl_processor.extract(self.database, up_to_ts)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def load(self):
        """Load data into the target database"""
        result = self.etl_processor.load(self.database, self.corruptor, self.metrics_tracker)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def random_operation(self):
        """Perform a random operation on the source database"""
        result = self.database.random_operation(self.metrics_tracker, self.etl_processor.sync_state)
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def seatbelt_check(self):
        """Perform validation between source and target databases"""
        result = self.validation_engine.seatbelt_check(self.database, self.etl_processor, self.metrics_tracker)
        self.last_seatbelt_check_ts = self.database.source_sequence_no
        if self.on_data_changed:
            self.on_data_changed()
        return result
    
    def find(self, id):
        """Find all operations for a specific ID"""
        return self.database.find(id)
    
    def trace(self, id):
        """Enable tracing for a specific ID"""
        return self.etl_processor.trace(id, self.database)
    
    def get_state(self):
        """Get the current state of the simulator"""
        return {
            'metrics': self.metrics_tracker.get(),
            'source_db': self.database.source_db,
            'target_db': self.database.target_db,
            'staging': self.etl_processor.staging,
            'sync_state': self.etl_processor.sync_state,
            'corrupt_filter': self.corruptor.corrupt_filter,
            'corrupt_nulls': self.corruptor.corrupt_nulls,
            'corruptible_columns': self.etl_processor.null_corruptible_columns,
            'seatbelt': self.validation_engine.seatbelt,
            'last_modified_row_id': self.database.last_modified_row_id,
            'schema': self.database.schema,
        }
    
    def run_simulation(self, plan=None):
        """Run a simulation with a predefined or default plan"""
        # Create Plan if none provided
        if plan is None:
            plan = self._create_default_plan()
        
        # Set simulation state
        self.simulation_running = True
        self.pause_simulation = False
        
        # Execute Plan
        for step in plan:
            if not self.simulation_running:
                break
                
            if self.pause_simulation:
                continue
                
            # Execute the step
            step()
            
            # Check if it's time for a seatbelt check
            if (self.database.source_sequence_no - self.last_seatbelt_check_ts > 
                    (self.seatbelt_interval + self.seatbelt_check_sleep)):
                if self.etl_processor.sync_state['last_load_ts'] > self.last_seatbelt_check_ts:
                    self.seatbelt_check()
                    self.last_seatbelt_check_ts = self.database.source_sequence_no
                    self.seatbelt_check_sleep = 0
                else:
                    logging.info(f"Waiting for load to complete before next seatbelt check")
                    self.seatbelt_check_sleep += 10
        
        # Reset simulation state
        self.simulation_running = False
        
    def _create_default_plan(self) -> List[Callable[[], None]]:
        """Create a default simulation plan"""
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
        
        # Configure NULL corruption for a specific column
        nullable_columns = [col.name for col in self.database.schema.columns 
                           if col.nullable and col.name != 'id']
        if nullable_columns:
            first_nullable = nullable_columns[0]
            plan.append(lambda: self.set_null_corruption_for_column(first_nullable, True))
            
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
                
        return plan
    
    def create_plan_from_config(self, config: Dict[str, Any]) -> List[Callable[[], None]]:
        """Create a simulation plan from a configuration dictionary
        
        Args:
            config: Configuration dictionary with a 'plan' key
            
        Returns:
            List of plan steps (callables)
        """
        if 'plan' not in config:
            return self._create_default_plan()
            
        plan_config = config['plan']
        plan = []
        
        for step_config in plan_config:
            if 'operation' not in step_config:
                logging.warning(f"Skipping plan step without operation: {step_config}")
                continue
                
            operation = step_config['operation']
            
            if operation == 'insert':
                custom_values = step_config.get('values')
                plan.append(lambda values=custom_values: self.insert_row(values))
            elif operation == 'insert_with_null':
                column = step_config.get('column')
                plan.append(lambda col=column: self.insert_with_null(col))
            elif operation == 'update':
                row_id = step_config.get('id')
                custom_values = step_config.get('values')
                plan.append(lambda id=row_id, values=custom_values: self.update_row(id, values))
            elif operation == 'update_with_null':
                column = step_config.get('column')
                plan.append(lambda col=column: self.update_with_null(col))
            elif operation == 'delete':
                row_id = step_config.get('id')
                plan.append(lambda id=row_id: self.delete_row(id))
            elif operation == 'extract':
                plan.append(lambda: self.extract())
            elif operation == 'load':
                plan.append(lambda: self.load())
            elif operation == 'seatbelt_check':
                plan.append(lambda: self.seatbelt_check())
            elif operation == 'corrupt_by_insert':
                plan.append(lambda: self.corrupt_by_insert())
            elif operation == 'corrupt_by_update':
                plan.append(lambda: self.corrupt_by_update())
            elif operation == 'corrupt_by_delete':
                plan.append(lambda: self.corrupt_by_delete())
            elif operation == 'corrupt_target':
                # Check if a specific row is provided for corruption
                if 'row' in step_config:
                    row_data = step_config['row'].copy()
                    # Extract row_id if specified in the row data
                    row_id = row_data.get('id')
                    if row_id is not None:
                        plan.append(lambda id=row_id, data=row_data: self.corrupt_target_with_row(id, data))
                    else:
                        logging.warning(f"corrupt_target operation requires an 'id' field in the row: {step_config}")
                        plan.append(lambda: self.corrupt_target_score())
                else:
                    # Fall back to random corruption
                    plan.append(lambda: self.corrupt_target_score())
            elif operation == 'toggle_null_corruption':
                column = step_config.get('column')
                plan.append(lambda col=column: self.toggle_null_corruption(col))
            elif operation == 'set_null_corruption':
                column = step_config.get('column')
                enabled = step_config.get('enabled', True)
                plan.append(lambda col=column, en=enabled: self.set_null_corruption_for_column(col, en))
            elif operation == 'random':
                plan.append(lambda: self.random_operation())
            elif operation == 'trace':
                # Trace a specific ID or the result of another operation
                if 'id' in step_config:
                    row_id = step_config['id']
                    plan.append(lambda id=row_id: self.trace(id))
                elif 'operation' in step_config.get('after', {}):
                    # Create a nested operation that will be traced
                    nested_op = step_config['after']
                    if nested_op['operation'] == 'insert':
                        values = nested_op.get('values')
                        plan.append(lambda vals=values: self.trace(self.insert_row(vals)))
                    elif nested_op['operation'] == 'random':
                        plan.append(lambda: self.trace(self.random_operation()))
                    # Add more nested operations as needed
            else:
                logging.warning(f"Unknown operation in plan: {operation}")
        
        return plan
    
    def stop_simulation(self):
        """Stop an ongoing simulation"""
        self.simulation_running = False
    
    def pause_resume_simulation(self):
        """Pause or resume an ongoing simulation"""
        self.pause_simulation = not self.pause_simulation
        return self.pause_simulation
    
    @classmethod
    def from_config_file(cls, config_file: Union[str, Path]) -> 'Simulator':
        """Create a Simulator instance from a configuration file
        
        Args:
            config_file: Path to a configuration file
            
        Returns:
            A new Simulator instance configured according to the file
        """
        return cls(config_file=config_file) 