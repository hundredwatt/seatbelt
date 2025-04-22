"""ETL (Extract-Transform-Load) operations for the Seatbelt Demo simulator."""

import logging
from typing import Dict, List, Any, Optional, Set
from .transformations import Transformations
from .config import TRACING_IDS

class ETLProcessor:
    """Class responsible for ETL (Extract-Transform-Load) operations"""

    def __init__(self):
        self.staging = []
        self.sync_state = {
            'last_extract_ts': -1,
            'last_load_ts': -1,
        }
        # Track which columns can have NULL corruptions
        self.null_corruptible_columns: Set[str] = set()

    def extract(self, database, up_to_ts=None):
        """Extract data from source database"""
        # If no timestamp is provided, use the current sequence number
        if up_to_ts is None:
            up_to_ts = database.source_sequence_no

        incremental = [
            row for row in database.source_db_log
            if row['ts'] > self.sync_state['last_extract_ts']
        ]
        self.staging.extend(incremental)

        # Single log message with all relevant info
        logging.info(f"EXTRACT: from_ts={self.sync_state['last_extract_ts']}, to_ts={up_to_ts}, extracted {len(incremental)} operations to staging")

        self.sync_state['last_extract_ts'] = up_to_ts
        database.source_sequence_no += 1

        for row in self.staging:
            if row['id'] in TRACING_IDS:
                logging.info(f"[TRACE] EXTRACT: id={row['id']}, ts={row['ts']}, deleted={row['deleted']}")

        return len(incremental)

    def transform_for_target(self, source_value: Any, column) -> Any:
        """Transform a value from source type to target type if needed"""
        # Convert database ColumnType to transformations ColumnType
        return Transformations.transform_source_to_target(
            source_value, 
            column.type, 
            column.target_type
        )

    def load(self, database, corruptor, metrics_tracker):
        """Load data into target database"""
        # Process the operations in staging
        if not self.staging:
            logging.info("LOAD: no data to load")
            return 0

        # Get original staging size before processing
        original_staging_size = len(self.staging)

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
                logging.debug(f"FILTERED: id={row_id} (blocked by corrupt filter)")
                continue

            count += 1

            if row.get('deleted', False):
                deletes.add(row_id)
                database.target_db.pop(row_id, None)

                if row_id in TRACING_IDS:
                    logging.info(f"[TRACE] LOAD - DELETE: id={row_id}, ts={row['ts']}, deleted={row['deleted']}")
            else:
                deletes.discard(row_id)
                # Create a copy without the 'deleted' field for target
                target_row = {'id': row_id}  # Start with just the ID
                
                # Apply source-to-target transformations for each column
                for column in database.schema.iter_target_columns():
                    col_name = column.name

                    # Handle target-only computed columns first
                    if column.target_only and column.computed_from:
                        operation = column.computed_from.get('operation')
                        arguments = column.computed_from.get('arguments', [])
                        source_columns = column.computed_from.get('source_columns', arguments)

                        if operation and source_columns:
                            source_values = [row.get(src_col) for src_col in source_columns]
                            computed_value = Transformations.apply_computed_operation(
                                operation, source_values, arguments, column.target_type or column.type
                            )
                            target_row[col_name] = computed_value
                        # Whether or not computed value generated, proceed to next column
                        continue

                    if column.target_only:
                        # Skip other target-only columns (without computed_from)
                        continue

                    # For regular columns synced from source
                    source_value = row.get(col_name)

                    # NULL corruption logic
                    if corruptor.corrupt_nulls and source_value is None and col_name in self.null_corruptible_columns:
                        target_row[col_name] = database._generate_default_value(
                            column.target_type if column.target_type else column.type
                        )
                        null_corrupted_count += 1
                        logging.debug(f"NULL CORRUPTED: id={row_id}, column={col_name} (NULL Mismap)")
                    else:
                        target_row[col_name] = self.transform_for_target(source_value, column)

                database.target_db[row_id] = target_row

                if row_id in TRACING_IDS:
                    logging.info(f"[TRACE] LOAD - UPSERT: id={row_id}, ts={row['ts']}, deleted={row['deleted']}, row={target_row}")

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

        # Build a single log message with all relevant information
        status_parts = []
        status_parts.append(f"processing {original_staging_size} operations")

        if count > 0:
            status_parts.append(f"{count} operations loaded")
        if filtered_count > 0:
            status_parts.append(f"{filtered_count} filtered out")
        if null_corrupted_count > 0:
            status_parts.append(f"{null_corrupted_count} NULL values mismapped")

        logging.info(f"LOAD: {', '.join(status_parts)}")

        return count

    def set_null_corruptible_columns(self, columns: List[str]) -> None:
        """Set which columns can have NULL values corrupted during loading"""
        self.null_corruptible_columns = set(columns)

    def add_null_corruptible_column(self, column_name: str) -> None:
        """Add a column to the list of columns that can have NULL values corrupted"""
        self.null_corruptible_columns.add(column_name)

    def clear_null_corruptible_columns(self) -> None:
        """Clear the list of columns that can have NULL values corrupted"""
        self.null_corruptible_columns.clear()

    def trace(self, id, database):
        """Enable tracing for a specific ID"""
        if id not in TRACING_IDS:
            TRACING_IDS.append(id)
            logging.info(f"[TRACE] Added tracing for id={id}")

        logging.info(f"[TRACE] source_db={database.find(id)}")
        logging.info(f"[TRACE] target_db={database.target_db.get(id, None)}")
        return id
