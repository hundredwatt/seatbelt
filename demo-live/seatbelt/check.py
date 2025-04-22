#!/usr/bin/env python3
"""
Seatbelt validation script for checking data consistency between MySQL and PostgreSQL.
This script uses pyseatbelt to validate data between the source (MySQL) and target (PostgreSQL) databases.
"""

import os
import sys
import logging
import time
import pymysql
import psycopg2
import psycopg2.extras
import hashlib
from datetime import datetime
from pathlib import Path
from tabulate import tabulate
from colorama import Fore, Style, init

# Initialize colorama
init()

# Add pyseatbelt to the path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', '..', 'pyseatbelt'))

# Import pyseatbelt classes
from pyseatbelt.validation import Source, Target, ValidationEngine, ValidationStatus, Operation
from pyseatbelt.config import TRACING_IDS

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


# Database connection parameters from docker-compose.yml
MYSQL_CONFIG = {
    'host': 'localhost',
    'port': 55800,
    'user': 'mysqluser',
    'password': 'mysqlpw',
    'db': 'mysql_db'
}

POSTGRES_CONFIG = {
    'host': 'localhost',
    'port': 55802,
    'user': 'postgres',
    'password': 'postgres',
    'dbname': 'sling'
}

# Column names to check - make sure these exist in both databases
COLUMNS = ['name', 'score', 'price', 'temperature', 'timestamp', 'text', 'test_name']


class MysqlSource(Source):
    """Implementation of Source for MySQL database."""
    
    def __init__(self):
        """Initialize the MySQL source."""
        self.conn = None
        self.connect()
    
    def connect(self):
        """Connect to the MySQL database."""
        try:
            self.conn = pymysql.connect(
                host=MYSQL_CONFIG['host'],
                port=MYSQL_CONFIG['port'],
                user=MYSQL_CONFIG['user'],
                password=MYSQL_CONFIG['password'],
                database=MYSQL_CONFIG['db']
            )
            logger.info("Connected to MySQL source database")
        except Exception as e:
            logger.error(f"Failed to connect to MySQL: {str(e)}")
            raise
    
    def read_change_log_changes(self, column_names):
        """Read changes from the MySQL database.
        
        For this implementation, we don't have a change log table, so we return an empty dictionary.
        In a real-world scenario, you might read from a CDC log or similar source.
        """
        if not self.conn:
            self.connect()

        signature_map = {}
        cursor = self.conn.cursor()

        # Use COALESCE to handle NULL values
        cols = ", ".join([f"COALESCE({col}, '')" for col in column_names])
        query = f"SELECT id, {cols} FROM demo_data"
        
        try:
            cursor.execute(query)
            for row in cursor.fetchall():
                if row and row[0] is not None:
                    row_id = row[0]
                    column_values = row[1:]
                    source_signature = hashlib.md5(''.join(str(value) for value in column_values).encode()).hexdigest()[:16]
                    
                    target_values = list(row[1:])
                    score_index = column_names.index('score')
                    # Only add '+' to exponents that don't already have a sign
                    target_values[score_index] = str(target_values[score_index]).replace('e', 'e+').replace('e+-', 'e-')
                    target_signature = hashlib.md5(''.join(str(value) for value in target_values).encode()).hexdigest()[:16]
                    
                    signature_map[row_id] = (source_signature, target_signature)

                    if row_id in TRACING_IDS:
                        logger.info(f"[TRACE] ID: {row_id}")
                        logger.info(f"[TRACE] Column values: {column_values}")
                        logger.info(f"[TRACE] Source signature: {source_signature}")
                        logger.info(f"[TRACE] Target signature: {target_signature}")
            return signature_map
        except Exception as e:
            logger.error(f"Error reading change log from MySQL: {str(e)}")
            #return {}
            raise e
        finally:
            cursor.close()
    
    def read_signatures(self, column_names):
        """Read signatures from the MySQL database."""
        if not self.conn:
            self.connect()
        
        signatures = {}
        cursor = self.conn.cursor()
        
        # Build query selecting only the specified columns
        # Use COALESCE to handle NULL values
        cols = [f"COALESCE({col}, '')" for col in column_names]
        column_str = ', '.join(cols)
        query = f"SELECT id, MD5(CONCAT({column_str})), CONCAT({column_str}) FROM demo_data"
        if any(TRACING_IDS):
            logger.info(f"[TRACE] query={query}")

        try:
            cursor.execute(query)
            for row in cursor.fetchall():
                if row[0] is not None and row[1] is not None:
                    row_id = row[0]
                    signature = row[1][:16]
                    signatures[row_id] = signature

                    if row_id in TRACING_IDS:
                        logger.info(f"[TRACE] id={row_id} mysql_signature={signature} column_str={row[2]}")
                
            logger.debug(f"Read {len(signatures)} signatures from MySQL source")
            return signatures
        except Exception as e:
            logger.error(f"Error reading signatures from MySQL: {str(e)}")
            #return {}
            raise e
        finally:
            cursor.close()
    
    def get_row_data(self, row_id, column_names):
        """Fetch a specific row from MySQL database."""
        if not self.conn:
            self.connect()
            
        cursor = self.conn.cursor(pymysql.cursors.DictCursor)
        columns = ", ".join(column_names)
        query = f"SELECT {columns} FROM demo_data WHERE id = %s"
        
        try:
            cursor.execute(query, (row_id,))
            result = cursor.fetchone()
            return result
        except Exception as e:
            logger.error(f"Error fetching row from MySQL: {str(e)}")
            return None
        finally:
            cursor.close()
    
    def close(self):
        """Close the MySQL connection."""
        if self.conn:
            try:
                self.conn.close()
                logger.debug("MySQL connection closed")
            except Exception as e:
                logger.warning(f"Error closing MySQL connection: {str(e)}")


class PostgresTarget(Target):
    """Implementation of Target for PostgreSQL database."""
    
    def __init__(self):
        """Initialize the PostgreSQL target."""
        self.conn = None
        self.connect()
    
    def connect(self):
        """Connect to the PostgreSQL database."""
        try:
            self.conn = psycopg2.connect(
                host=POSTGRES_CONFIG['host'],
                port=POSTGRES_CONFIG['port'],
                user=POSTGRES_CONFIG['user'],
                password=POSTGRES_CONFIG['password'],
                dbname=POSTGRES_CONFIG['dbname']
            )
            logger.info("Connected to PostgreSQL target database")
        except Exception as e:
            logger.error(f"Failed to connect to PostgreSQL: {str(e)}")
            raise
    
    def read_signatures(self, column_names):
        """Read signatures from the PostgreSQL database."""
        if not self.conn:
            self.connect()
        
        signatures = {}
        cursor = self.conn.cursor()
        
        # Build query selecting only the specified columns
        # The target table in PostgreSQL is in the 'sling' schema with '{stream_schema}_{stream_table}' naming
        # Use COALESCE to handle NULL values
        pg_types = {
            'score': 'real::text',
        }
        cols = [f"COALESCE({col}::{pg_types.get(col, 'text')}, '')" for col in column_names]
        column_str = ' || '.join(cols)
        # Adjusted for PostgreSQL table naming convention
        query = f"SELECT id, MD5({column_str}), {column_str} FROM sling.mysql_db_demo_data"
        if any(TRACING_IDS):
            logger.info(f"[TRACE] query={query}")
        
        try:
            cursor.execute(query)
            for row in cursor.fetchall():
                if row[0] is not None and row[1] is not None:
                    row_id = row[0]
                    signature = row[1][:16]
                    signatures[row_id] = signature

                    if row_id in TRACING_IDS:
                        logger.info(f"[TRACE] id={row_id} postgres_signature={signature} column_str={row[2]}")
                
            logger.debug(f"Read {len(signatures)} signatures from PostgreSQL target")
            return signatures
        except Exception as e:
            logger.error(f"Error reading signatures from PostgreSQL: {str(e)}")
            #return {}
            raise e
        finally:
            cursor.close()
    
    def get_row_data(self, row_id, column_names):
        """Fetch a specific row from PostgreSQL database."""
        if not self.conn:
            self.connect()
            
        cursor = self.conn.cursor(cursor_factory=psycopg2.extras.DictCursor)
        columns = ", ".join(column_names)
        query = f"SELECT {columns} FROM sling.mysql_db_demo_data WHERE id = %s"
        
        try:
            cursor.execute(query, (row_id,))
            result = cursor.fetchone()
            return dict(result) if result else None
        except Exception as e:
            logger.error(f"Error fetching row from PostgreSQL: {str(e)}")
            return None
        finally:
            cursor.close()
    
    def close(self):
        """Close the PostgreSQL connection."""
        if self.conn:
            try:
                self.conn.close()
                logger.debug("PostgreSQL connection closed")
            except Exception as e:
                logger.warning(f"Error closing PostgreSQL connection: {str(e)}")


def generate_diff(source_data, target_data):
    """Generate a colored diff between source and target row data."""
    if not source_data or not target_data:
        return "Cannot generate diff: missing data"
    
    diff_lines = []
    for key in sorted(set(list(source_data.keys()) + list(target_data.keys()))):
        if key not in source_data:
            diff_lines.append(f"{Fore.GREEN}+ {key}: {target_data[key]}{Style.RESET_ALL}")
        elif key not in target_data:
            diff_lines.append(f"{Fore.RED}- {key}: {source_data[key]}{Style.RESET_ALL}")
        elif source_data[key] != target_data[key]:
            diff_lines.append(f"{Fore.YELLOW}~ {key}:{Style.RESET_ALL}")
            diff_lines.append(f"{Fore.RED}- {source_data[key]}{Style.RESET_ALL}")
            diff_lines.append(f"{Fore.GREEN}+ {target_data[key]}{Style.RESET_ALL}")
    
    return "\n".join(diff_lines) if diff_lines else "No differences found"


def main():
    """Run the validation between MySQL and PostgreSQL."""
    # Create seatbelt data directory if it doesn't exist
    seatbelt_data_dir = Path("data/seatbelt")
    seatbelt_data_dir.mkdir(parents=True, exist_ok=True)
    
    # Generate seatbelt data file path with timestamp
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    seatbelt_data_file = seatbelt_data_dir / f"seatbelt_{timestamp}.json"
    
    # Create validation engine
    # Check if previous seatbelt data file exists and load it
    previous_seatbelt_data_files = list(seatbelt_data_dir.glob("seatbelt_*.json"))
    previous_seatbelt_data_file = max(previous_seatbelt_data_files, default=None, key=lambda x: x.stat().st_mtime)
    
    engine = None
    if previous_seatbelt_data_file:
        logger.info(f"Loading previous seatbelt file")
        engine = ValidationEngine(shadow_file=str(previous_seatbelt_data_file))
    else:
        logger.info("No previous seatbelt file found, starting fresh")
        engine = ValidationEngine()
    
    # Create source and target
    source = MysqlSource()
    target = PostgresTarget()
    
    try:
        # Run validation
        logger.info("Running seatbelt validation...")
        metrics = engine.seatbelt_check(source, target, column_names=COLUMNS)
        
        # Save seatbelt_data to file
        engine.save_shadow(str(seatbelt_data_file))
        
        # Print metrics as a table
        print("\nValidation Results:")
        metrics_table = [
            ["Source size", metrics['source_size']],
            ["Target size", metrics['target_size']],
            ["Valid rows", metrics['valid_count']],
            ["Pending rows", metrics['pending_count']],
            ["Discrepant rows", f"{metrics['error_count']}{Fore.RED} ! {Style.RESET_ALL}" if metrics['error_count'] > 0 else metrics['error_count']]
        ]
        print(tabulate(metrics_table, tablefmt="grid"))
        
        # Categorize issues
        source_only_ids = []
        target_only_ids = []
        drifted_ids = []
        error_entries = {id: entry for id, entry in engine.shadow.items() if entry['validation_error'] == True}
        
        for id, entry in error_entries.items():
            source_present = entry['source_operation'] not in [Operation.DELETE, Operation.DOES_NOT_EXIST]
            target_present = entry['target_operation'] not in [Operation.DELETE, Operation.DOES_NOT_EXIST]
            if source_present and target_present:
                drifted_ids.append(id)
            elif source_present:
                source_only_ids.append(id)
            elif target_present:
                target_only_ids.append(id)

        pending_ids = [id for id, entry in engine.shadow.items() if entry['validation_status'] == ValidationStatus.PENDING]
        
        # Build detailed table data
        table_data = []
        
        # Add source only rows
        for id in sorted(source_only_ids):
            table_data.append([
                id, 
                f"{Fore.RED}ERROR{Style.RESET_ALL}", 
                "Source Only", 
                ""
            ])
        
        # Add target only rows
        for id in sorted(target_only_ids):
            table_data.append([
                id, 
                f"{Fore.RED}ERROR{Style.RESET_ALL}", 
                "Target Only", 
                ""
            ])
        
        # Add drifted rows with diff
        for id in sorted(drifted_ids):
            source_data = source.get_row_data(id, COLUMNS)
            target_data = target.get_row_data(id, COLUMNS)
            diff = generate_diff(source_data, target_data)
            
            table_data.append([
                id, 
                f"{Fore.RED}ERROR{Style.RESET_ALL}", 
                "Drifted", 
                diff
            ])
        
        # Add pending rows
        for id in sorted(pending_ids):
            table_data.append([
                id, 
                f"{Fore.YELLOW}PENDING{Style.RESET_ALL}", 
                "", 
                ""
            ])
        
        if table_data:
            print("\nRow Details:")
            print(tabulate(table_data, headers=["ID", "Status", "Discrepancy Type", "Diff"], tablefmt="grid"))
        else:
            print("\nNo validation issues found!")
        
        for id, entry in engine.shadow.items():
            if id in TRACING_IDS:
                logger.info(f"ID: {id}")
                logger.info(f"Entry: {entry}")
        
        return 0
    except Exception as e:
        logger.error(f"Validation failed: {str(e)}")
        raise e
    finally:
        # Clean up connections
        source.close()
        target.close()


if __name__ == "__main__":
    sys.exit(main())
