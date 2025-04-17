#!/usr/bin/env python3
"""
Custom Schema Simulation Example for Seatbelt Demo

This example demonstrates how to use the Seatbelt Demo simulator
with a custom schema defined in a configuration file.
"""

import logging
import argparse
import time
import sys
import os
from datetime import datetime, date
from pathlib import Path

# Add parent directory to path for imports
sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from seatbelt_demo.simulator import Simulator
from seatbelt_demo.simulator.database import SchemaDefinition, ColumnDefinition, InitialData, ColumnType
from seatbelt_demo.simulator.config import load_simulator_config, save_config_to_file

# Configure logging to output to console
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)

def parse_args():
    """Parse command-line arguments."""
    parser = argparse.ArgumentParser(description='Seatbelt Demo Custom Schema Simulation')
    parser.add_argument('--config', type=str, help='Path to configuration file')
    parser.add_argument('--save-config', type=str, help='Save default config to this file')
    parser.add_argument('--seed', type=int, default=42, help='Random seed for reproducibility')
    parser.add_argument('--pause', type=float, default=0.1, help='Pause between operations (seconds)')
    return parser.parse_args()

def create_custom_schema_programmatically():
    """Create a custom schema programmatically (without a config file)"""
    schema = SchemaDefinition()
    
    # Add first_name column
    schema.add_column(ColumnDefinition(
        name='first_name',
        type=ColumnType.STRING,
        nullable=False,
        generator=lambda: ['Alice', 'Bob', 'Charlie', 'David', 'Emma'][int(time.time() * 10) % 5]
    ))
    
    # Add last_name column
    schema.add_column(ColumnDefinition(
        name='last_name',
        type=ColumnType.STRING,
        nullable=False
    ))
    
    # Add balance column (float with custom generator)
    schema.add_column(ColumnDefinition(
        name='balance',
        type=ColumnType.FLOAT,
        nullable=True,
        generator=lambda: round(1000 * (time.time() % 1), 2)
    ))
    
    # Add last_visit column (date)
    schema.add_column(ColumnDefinition(
        name='last_visit',
        type=ColumnType.DATE,
        nullable=True
    ))
    
    return schema

def create_initial_data():
    """Create initial data for the simulation"""
    initial_data = InitialData(row_count=3)
    
    # Add some predefined rows
    initial_data.add_row({
        'first_name': 'John',
        'last_name': 'Doe',
        'balance': 1234.56,
        'last_visit': date(2023, 1, 15)
    })
    
    initial_data.add_row({
        'first_name': 'Jane',
        'last_name': 'Smith',
        'balance': None,  # NULL value
        'last_visit': date(2023, 2, 20)
    })
    
    return initial_data

def main():
    """Run the custom schema simulation."""
    args = parse_args()
    
    # Option to save default config to file and exit
    if args.save_config:
        # Create a schema
        schema = create_custom_schema_programmatically()
        initial_data = create_initial_data()
        
        # Create config
        config = {
            'schema': schema,
            'initial_data': initial_data,
            'random_seed': args.seed,
            'seatbelt_interval': 25
        }
        
        # Save to file
        save_config_to_file(config, args.save_config)
        print(f"Configuration saved to {args.save_config}")
        return
    
    # Create simulator either from config file or programmatically
    if args.config:
        # Load from config file
        logging.info(f"Loading configuration from {args.config}")
        simulator = Simulator.from_config_file(args.config)
    else:
        # Create programmatically
        logging.info("Using programmatically defined schema")
        schema = create_custom_schema_programmatically()
        initial_data = create_initial_data()
        simulator = Simulator(args.seed, schema, initial_data)
    
    # Configure custom data generators if needed (for faker or custom functions)
    if not args.config:
        # Set custom generators
        schema = simulator.database.schema
        
        # Add custom generator for email column if it exists
        email_column = schema.get_column_by_name('email')
        if email_column:
            email_column.generator = lambda: f"{simulator.database.fake.user_name()}@example.com"
            
        # Add custom generator for date columns if they exist
        date_column = schema.get_column_by_name('last_visit')
        if date_column:
            date_column.generator = lambda: simulator.database.fake.date_between(
                start_date='-1y', end_date='today'
            )
    
    # Run simulation with default plan
    logging.info("Starting simulation")
    
    # If using config file and it has a plan, use it, otherwise use default plan
    plan = None
    if args.config:
        try:
            config = load_simulator_config(args.config)
            if 'plan' in config:
                logging.info("Using plan from configuration file")
                plan = simulator.create_plan_from_config(config)
        except Exception as e:
            logging.error(f"Error loading plan from config: {e}")
            plan = None
    
    try:
        # Run some custom operations to demonstrate the schema
        logging.info(f"Schema has {len(simulator.database.schema.columns)} columns")
        for column in simulator.database.schema.columns:
            logging.info(f"Column: {column.name}, Type: {column.type.value}, Nullable: {column.nullable}")
        
        # Insert a row with custom values
        row_id = simulator.insert_row({
            'first_name': 'Alex',
            'last_name': 'Taylor',
        })
        logging.info(f"Inserted row with ID: {row_id}")
        
        # Insert a row with NULL in a nullable column
        nullable_columns = [col.name for col in simulator.database.schema.columns 
                           if col.nullable and col.name != 'id']
        
        if nullable_columns:
            null_column = nullable_columns[0]
            row_id = simulator.insert_with_null(null_column)
            logging.info(f"Inserted row with NULL in {null_column}, ID: {row_id}")
            
            # Enable NULL corruption for this column
            simulator.set_null_corruption_for_column(null_column, True)
            logging.info(f"Enabled NULL corruption for column {null_column}")
        
        # Extract and load
        simulator.extract()
        simulator.load()
        
        # Corrupt a target value
        simulator.corrupt_target_score()
        
        # Run validation
        simulator.seatbelt_check()
        
        # Run simulation plan if available
        if plan:
            logging.info(f"Running simulation plan with {len(plan)} steps")
            simulator.run_simulation(plan)
        else:
            logging.info("Using default simulation plan")
            simulator.run_simulation()
            
    except KeyboardInterrupt:
        logging.info("Simulation interrupted by user")
    
    finally:
        # Print final status
        state = simulator.get_state()
        metrics = state['metrics']
        
        logging.info("=== Simulation Summary ===")
        logging.info(f"Source DB: {len(state['source_db'])} rows")
        logging.info(f"Target DB: {len(state['target_db'])} rows")
        logging.info(f"Operations: {metrics['source_ops_count']} source, {metrics['target_ops_count']} target")
        logging.info(f"Corruptions: {metrics['corruption_count']}")
        
        valid_count = metrics.get('valid_count', 0)
        error_count = metrics.get('error_count', 0)
        pending_count = metrics.get('pending_count', 0)
        total_validated = valid_count + error_count + pending_count
        
        if total_validated > 0:
            logging.info(f"Validation: {valid_count} valid, {error_count} errors, {pending_count} pending")

if __name__ == '__main__':
    main() 