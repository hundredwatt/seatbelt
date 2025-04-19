"""ETL (Extract-Transform-Load) operations for the Seatbelt Demo simulator."""

import logging
from typing import Dict, List, Any, Optional, Set

class ETLProcessor:
    """Class responsible for ETL (Extract-Transform-Load) operations"""

    def __init__(self):
        self.staging = []
        self.sync_state = {
            'last_extract_ts': -1,
            'last_load_ts': -1,
        }
        self.tracing_ids = []
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
            if row['id'] in self.tracing_ids:
                logging.info(f"[TRACE] EXTRACT: id={row['id']}, ts={row['ts']}, deleted={row['deleted']}")

        return len(incremental)

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

                # Apply type transformations and handle NULL corruptions
                for column in database.schema.columns:
                    col_name = column.name
                    if col_name == 'id':
                        continue  # Skip id column

                    # Get the source value
                    source_value = target_row.get(col_name)

                    # Check for NULL values and corrupt if necessary
                    if corruptor.corrupt_nulls and source_value is None and col_name in self.null_corruptible_columns:
                        # Replace NULL with appropriate default value based on column type
                        target_row[col_name] = database._generate_default_value(
                            column.target_type if column.target_type else column.type
                        )
                        null_corrupted_count += 1
                        logging.debug(f"NULL CORRUPTED: id={row_id}, column={col_name} (NULL Mismap)")
                    elif column.target_type and column.target_type != column.type:
                        # Apply type transformation if the column has a target type different from source
                        target_row[col_name] = database._transform_for_target(source_value, column)

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
        if id not in self.tracing_ids:
            self.tracing_ids.append(id)
            logging.info(f"[TRACE] Added tracing for id={id}")

        logging.info(f"[TRACE] source_db={database.find(id)}")
        logging.info(f"[TRACE] target_db={database.target_db.get(id, None)}")
        return id
