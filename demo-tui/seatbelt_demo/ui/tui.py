"""Terminal User Interface for Seatbelt Demo."""

import curses
import time
import threading
from datetime import datetime
import copy
from curses import wrapper
import random
import logging
import sys
import hashlib
import json
import argparse
import os
from typing import List, Dict, Any, Optional, Set, Tuple, Callable

from ..simulator import Simulator
from ..simulator.schema_utils import convert_schema_dict, convert_initial_data_dict

# Configure logging to capture messages
class TUILogHandler(logging.Handler):
    def __init__(self):
        super().__init__()
        self.logs = []
        self.lock = threading.RLock()  # Use RLock instead of Lock
        
    def emit(self, record):
        try:
            msg = self.format(record)
            # Use non-blocking acquire with timeout
            if self.lock.acquire(blocking=False):
                try:
                    self.logs.append(msg)
                    if len(self.logs) > 100:  # Keep only the last 100 logs
                        self.logs.pop(0)
                finally:
                    self.lock.release()
        except Exception:
            self.handleError(record)

# Global state for TUI
class TUIState:
    def __init__(self):
        # Lock for thread safety
        self.lock = threading.Lock()
        
        # The simulator instance
        self.simulator = None
        
        # UI state variables
        self.logs = []
        self.last_modified_row_id = None
        self.recently_loaded_ids = set()
        self.last_load_time = 0
        
        # Add keyboard buffer
        self.key_buffer = []  # List to store last 32 key presses
        self.last_key_activity = time.time()
        
        # Add seatbelt animation state
        self.seatbelt_animation_state = {
            "active": False,
            "step": 0,
            "start_time": 0,
            "source_rows_read": 0,
            "target_rows_read": 0,
            "paused_until": 0,
            "completed": False,
            "new_metrics": {"error_count": 0, "pending_count": 0, "valid_count": 0}
        }
        
        # Add plan execution state
        self.plan_execution_state = {
            "active": False,
            "plan": None,
            "current_step": 0,
            "total_steps": 0,
            "waiting_for_input": False
        }

# UI Drawing Helper Functions
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

def setup_logging():
    """Set up logging configuration."""
    # Create and configure the TUI log handler first
    log_handler = TUILogHandler()
    log_handler.setLevel(logging.INFO)
    log_formatter = logging.Formatter('%(asctime)s - %(levelname)s - %(message)s', 
                                     datefmt='%H:%M:%S')
    log_handler.setFormatter(log_formatter)
    
    # Configure root logger - but be careful with existing handlers
    root_logger = logging.getLogger()
    # Remove existing handlers to avoid duplication
    for handler in root_logger.handlers[:]:
        root_logger.removeHandler(handler)
    
    # Set level but add handler separately
    root_logger.setLevel(logging.INFO)
    root_logger.addHandler(log_handler)
    
    # Get our module logger - no need to add handlers as it inherits from root
    logger = logging.getLogger("seatbelt_demo")
    
    # Disable propagation for some loggers to avoid loops
    logging.getLogger("asyncio").propagate = False
    
    return logger, log_handler

def parse_args():
    """Parse command-line arguments."""
    # Parse command-line arguments
    parser = argparse.ArgumentParser(description='Seatbelt Demo TUI')
    parser.add_argument('--check-only', action='store_true', help='Run initialization only and exit immediately (for error checking)')
    parser.add_argument('--config', type=str, help='Path to custom schema configuration file (YAML or JSON)')
    parser.add_argument('--no-plan', action='store_true', help='Do not run the plan even if one is included in the config file')
    return parser.parse_args()

def draw_source_db(stdscr, y, x, height, width, state, sim_state):
    """Draw the source database table."""
    draw_box(stdscr, y, x, height, width, "Source DB")

    # Get schema if available (for custom column display)
    schema = None
    if hasattr(state.simulator.database, 'schema'):
        schema = state.simulator.database.schema

    # Calculate available space for columns
    # Account for: left margin (2) + ID width (3) + separators + right margin (2)
    id_col_width = 3
    separator_width = 3  # " | "
    margin_width = 4  # left(2) + right(2)

    # Get non-ID columns
    non_id_columns = []
    if schema and hasattr(schema, 'columns'):
        non_id_columns = [col for col in schema.columns if col.name != 'id'][:2]  # Get up to 2 non-ID columns

    # Table headers
    if len(non_id_columns) == 2:
        # For 3-column schemas (ID + 2 data columns)
        # Calculate width for the two data columns
        col1_width = min(18, (width - id_col_width - separator_width * 2 - margin_width) // 2)
        col2_width = width - id_col_width - separator_width * 2 - margin_width - col1_width
        header = f"ID | {non_id_columns[0].name:<{col1_width}} | {non_id_columns[1].name}"
    elif len(non_id_columns) == 1:
        # For 2-column schemas (ID + 1 data column)
        data_col_name = non_id_columns[0].name
        header = f"ID | {data_col_name}"
    else:
        # Default schema
        header = "ID | data"
    
    add_str_safe(stdscr, y + 1, x + 2, header)
    add_str_safe(stdscr, y + 2, x + 2, "-" * (width - 4))

    # Display rows (up to height-4 rows to leave room for header and border)
    source_db = sim_state['source_db']
    rows = list(source_db.values())
    rows.sort(key=lambda r: r['id'], reverse=True)  # Show newest rows first

    max_rows = height - 4  # Leave room for header and borders
    for i, row in enumerate(rows[:max_rows]):
        if y + 3 + i >= y + height - 1:
            break

        # Format row based on schema
        if len(non_id_columns) == 2:
            # For 3-column schemas (ID + 2 data columns)
            col1 = non_id_columns[0].name
            col2 = non_id_columns[1].name
            
            # Format value for first data column
            val1 = row.get(col1)
            if val1 is None:
                val1_display = "NULL"
            elif isinstance(val1, (float, int)):
                val1_display = f"{val1}"
            else:
                val1_str = str(val1)
                if len(val1_str) > col1_width:
                    val1_display = val1_str[:col1_width]
                else:
                    val1_display = val1_str
            
            # Format value for second data column
            val2 = row.get(col2)
            if val2 is None:
                val2_display = "NULL"
            elif isinstance(val2, (float, int)):
                val2_display = f"{val2}"
            else:
                val2_str = str(val2)
                if len(val2_str) > col2_width:
                    val2_display = val2_str[:col2_width]
                else:
                    val2_display = val2_str
            
            row_str = f"{row['id']:<3} | {val1_display:<{col1_width}} | {val2_display}"
        elif len(non_id_columns) == 1:
            # For 2-column schemas (ID + 1 data column)
            col1 = non_id_columns[0].name
            
            # Format value for data column with all available space
            available_data_width = width - id_col_width - separator_width - margin_width
            val1 = row.get(col1)
            if val1 is None:
                val1_display = "NULL"
            elif isinstance(val1, (float, int)):
                val1_display = f"{val1}"
            else:
                val1_str = str(val1)
                if len(val1_str) > available_data_width:
                    val1_display = val1_str[:available_data_width]
                else:
                    val1_display = val1_str
            
            row_str = f"{row['id']:<3} | {val1_display}"
        else:
            # Default schema
            data_value = row.get('data')
            available_data_width = width - id_col_width - separator_width - margin_width
            if data_value is None:
                data_display = "NULL"
            else:
                data_str = str(data_value)
                if len(data_str) > available_data_width:
                    data_display = data_str[:available_data_width]
                else:
                    data_display = data_str
            
            row_str = f"{row['id']:<3} | {data_display}"

        # Only highlight the most recently modified row with green
        if row['id'] == state.last_modified_row_id:
            add_str_safe(stdscr, y + 3 + i, x + 2, row_str, curses.color_pair(2))
        else:
            add_str_safe(stdscr, y + 3 + i, x + 2, row_str)

def draw_target_db(stdscr, y, x, height, width, state, sim_state):
    """Draw the target database table."""
    draw_box(stdscr, y, x, height, width, "Target DB")

    # Get schema if available (for custom column display)
    schema = None
    if hasattr(state.simulator.database, 'schema'):
        schema = state.simulator.database.schema

    # Calculate available space for columns
    # Account for: left margin (2) + ID width (3) + separators + right margin (2)
    id_col_width = 3
    separator_width = 3  # " | "
    margin_width = 4  # left(2) + right(2)

    # Get non-ID columns
    non_id_columns = []
    if schema and hasattr(schema, 'columns'):
        non_id_columns = [col for col in schema.columns if col.name != 'id'][:2]  # Get up to 2 non-ID columns

    # Table headers
    if len(non_id_columns) == 2:
        # For 3-column schemas (ID + 2 data columns)
        # Calculate width for the two data columns
        col1_width = min(18, (width - id_col_width - separator_width * 2 - margin_width) // 2)
        col2_width = width - id_col_width - separator_width * 2 - margin_width - col1_width
        header = f"ID | {non_id_columns[0].name:<{col1_width}} | {non_id_columns[1].name}"
    elif len(non_id_columns) == 1:
        # For 2-column schemas (ID + 1 data column)
        data_col_name = non_id_columns[0].name
        header = f"ID | {data_col_name}"
    else:
        # Default schema
        header = "ID | data"
    
    add_str_safe(stdscr, y + 1, x + 2, header)
    add_str_safe(stdscr, y + 2, x + 2, "-" * (width - 4))

    # Display rows (up to height-4 rows to leave room for header and border)
    target_db = sim_state['target_db']
    rows = list(target_db.values())
    rows.sort(key=lambda r: r['id'], reverse=True)  # Show newest rows first

    max_rows = height - 4  # Leave room for header and borders
    for i, row in enumerate(rows[:max_rows]):
        if y + 3 + i >= y + height - 1:
            break

        # Format row based on schema
        if len(non_id_columns) == 2:
            # For 3-column schemas (ID + 2 data columns)
            col1 = non_id_columns[0].name
            col2 = non_id_columns[1].name
            
            # Format value for first data column
            val1 = row.get(col1)
            if val1 is None:
                val1_display = "NULL"
            elif isinstance(val1, (float, int)):
                val1_display = f"{val1}"
            else:
                val1_str = str(val1)
                if len(val1_str) > col1_width:
                    val1_display = val1_str[:col1_width]
                else:
                    val1_display = val1_str
            
            # Format value for second data column
            val2 = row.get(col2)
            if val2 is None:
                val2_display = "NULL"
            elif isinstance(val2, (float, int)):
                val2_display = f"{val2}"
            else:
                val2_str = str(val2)
                if len(val2_str) > col2_width:
                    val2_display = val2_str[:col2_width]
                else:
                    val2_display = val2_str
            
            row_str = f"{row['id']:<3} | {val1_display:<{col1_width}} | {val2_display}"
        elif len(non_id_columns) == 1:
            # For 2-column schemas (ID + 1 data column)
            col1 = non_id_columns[0].name
            
            # Format value for data column with all available space
            available_data_width = width - id_col_width - separator_width - margin_width
            val1 = row.get(col1)
            if val1 is None:
                val1_display = "NULL"
            elif isinstance(val1, (float, int)):
                val1_display = f"{val1}"
            else:
                val1_str = str(val1)
                if len(val1_str) > available_data_width:
                    val1_display = val1_str[:available_data_width]
                else:
                    val1_display = val1_str
            
            row_str = f"{row['id']:<3} | {val1_display}"
        else:
            # Default schema
            data_value = row.get('data')
            available_data_width = width - id_col_width - separator_width - margin_width
            if data_value is None:
                data_display = "NULL"
            else:
                data_str = str(data_value)
                if len(data_str) > available_data_width:
                    data_display = data_str[:available_data_width]
                else:
                    data_display = data_str
            
            row_str = f"{row['id']:<3} | {data_display}"

        # Highlight recently loaded rows in yellow (for 5 seconds after load)
        current_time = time.time()
        if row['id'] in state.recently_loaded_ids and current_time - state.last_load_time < 5:
            add_str_safe(stdscr, y + 3 + i, x + 2, row_str, curses.color_pair(3))  # Yellow for recently loaded
        else:
            add_str_safe(stdscr, y + 3 + i, x + 2, row_str)

def draw_pipeline(stdscr, y, x, height, width, state, sim_state):
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
    staging = sim_state['staging']
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
    target_db = sim_state['target_db']
    target_count = len(target_db)

    # Highlight target with cyan (border color)
    add_str_safe(stdscr, mid_y + 2, x + 5, f"Target DB ({target_count} rows)", curses.color_pair(1))

    # Draw pipeline status
    add_str_safe(stdscr, mid_y - 2, x + width - 20, "Pipeline Status:")
    if len(staging) > 0:
        add_str_safe(stdscr, mid_y, x + width - 20, f"READY TO LOAD: {len(staging)} operations", curses.color_pair(3))
    else:
        sync_state = sim_state['sync_state']
        add_str_safe(stdscr, mid_y, x + width - 20, f"Last LOAD TS: {sync_state['last_load_ts']}")

    # Show lag
    metrics = sim_state['metrics']
    if metrics["lag"] > 0:
        add_str_safe(stdscr, mid_y + 2, x + width - 20, f"LAG: {metrics['lag']} operations", curses.color_pair(4))

def draw_corrupt_filter(stdscr, y, x, height, width, state, sim_state):
    """Draw the corrupt filter box showing filtered IDs."""
    corrupt_filter = sim_state['corrupt_filter']
    corrupt_nulls = sim_state['corrupt_nulls']
    
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
    if display_title and y < max_y and x + 2 < max_x:
        title_len = min(len(display_title), width - 4)
        try:
            stdscr.addstr(y, x + 2, f" {display_title[:title_len]} ")
        except curses.error:
            pass

    stdscr.attroff(border_color)

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

def draw_seatbelt(stdscr, y, x, width, state, sim_state):
    """Draw the seatbelt component."""
    height = 15  # Increased height to fit the three new rows
    draw_box(stdscr, y, x, height, width, "Seatbelt")
    
    metrics = sim_state['metrics']
    seatbelt = sim_state['seatbelt']
    
    # Get the last check timestamp from simulator
    last_seatbelt_check_ts = state.simulator.last_seatbelt_check_ts
    
    # If no seatbelt check has been run yet and no animation is active, show an empty box
    if last_seatbelt_check_ts == 0 and not state.seatbelt_animation_state["active"]:
        return
        
    # Always show the animation steps, whether active or completed
    if state.seatbelt_animation_state["active"] or state.seatbelt_animation_state["completed"]:
        current_step = state.seatbelt_animation_state["step"]
        
        # Display the steps with appropriate colors
        step1_color = curses.color_pair(3) if current_step == 1 else (curses.color_pair(2) if current_step > 1 else None)
        step2_color = curses.color_pair(3) if current_step == 2 else (curses.color_pair(2) if current_step > 2 else None)
        step3_color = curses.color_pair(3) if current_step == 3 else (curses.color_pair(2) if current_step > 3 else None)
        # Always use green for the completion status when animation is finished
        complete_color = curses.color_pair(2)
        
        # Draw the steps
        add_str_safe(stdscr, y + 1, x + 2, "1. Reading Source DB Signatures", step1_color)
        if current_step >= 1 or state.seatbelt_animation_state["completed"]:
            add_str_safe(stdscr, y + 2, x + 5, f"→ {state.seatbelt_animation_state['source_rows_read']} rows read", step1_color)
            
        add_str_safe(stdscr, y + 3, x + 2, "2. Reading Target DB Signatures", step2_color)
        if current_step >= 2 or state.seatbelt_animation_state["completed"]:
            add_str_safe(stdscr, y + 4, x + 5, f"→ {state.seatbelt_animation_state['target_rows_read']} rows read", step2_color)
            
        add_str_safe(stdscr, y + 5, x + 2, "3. Processing", step3_color)
        if current_step >= 4 or state.seatbelt_animation_state["completed"]:
            add_str_safe(stdscr, y + 6, x + 5, "→ State Updated", complete_color)
            
        # Add a blank line after the steps
        # Line y + 7 is blank
        
        # After the steps are complete or if it's done, show the metrics too
        if not state.seatbelt_animation_state["active"] or current_step >= 4:
            # Display last check timestamp
            add_str_safe(stdscr, y + 8, x + 2, f"Last Check TS: {last_seatbelt_check_ts}")
            
            # Display metrics with updated terminology and order
            # First line: Valid Rows and Rows In-Flight
            add_str_safe(stdscr, y + 9, x + 2, f"Valid Rows: {metrics['valid_count']}   Rows In-Flight: {metrics['pending_count']}")
            
            # Second line: Rows Discrepant - use warning symbol and bold if there are errors
            if metrics["error_count"] > 0:
                discrepant_text = f"Rows Discrepant: {metrics['error_count']}"
                add_str_safe(stdscr, y + 10, x + 2, discrepant_text, curses.A_BOLD)
                add_str_safe(stdscr, y + 10, x + 2 + len(discrepant_text) + 1, "(!) ", curses.color_pair(4))
            else:
                add_str_safe(stdscr, y + 10, x + 2, f"Rows Discrepant: {metrics['error_count']}")
                
            # Display errors categorized into three types if any
            if metrics["error_count"] > 0:
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
        add_str_safe(stdscr, y + 3, x + 2, f"Valid Rows: {metrics['valid_count']}   Rows In-Flight: {metrics['pending_count']}")
        
        # Second line: Rows Discrepant - use warning symbol and bold if there are errors
        if metrics["error_count"] > 0:
            discrepant_text = f"Rows Discrepant: {metrics['error_count']}"
            add_str_safe(stdscr, y + 4, x + 2, discrepant_text, curses.A_BOLD)
            add_str_safe(stdscr, y + 4, x + 2 + len(discrepant_text) + 1, "(!) ", curses.color_pair(4))
        else:
            add_str_safe(stdscr, y + 4, x + 2, f"Rows Discrepant: {metrics['error_count']}")
            
        # Display errors categorized into three types if any
        if metrics["error_count"] > 0:
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

def draw_logs(stdscr, y, x, height, width, state):
    """Draw the log messages."""
    draw_box(stdscr, y, x, height, width, "Logs")
    
    # Display the most recent logs
    log_entries = state.logs[-height+2:]
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

def draw_metrics(stdscr, y, x, height, width, state, sim_state):
    """Draw the metrics."""
    draw_box(stdscr, y, x, height, width, "Metrics")
    
    metrics = sim_state['metrics']
    staging = sim_state['staging']
    sync_state = sim_state['sync_state']
    source_db = sim_state['source_db']
    target_db = sim_state['target_db']
    last_seatbelt_check_ts = state.simulator.last_seatbelt_check_ts
    source_sequence_no = state.simulator.database.source_sequence_no
    
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
    add_str_safe(stdscr, y + 16, x + 2, f"Valid Rows: {metrics['valid_count']}")
    
    # Show pending count without color highlighting
    add_str_safe(stdscr, y + 17, x + 2, f"Rows In-Flight: {metrics['pending_count']}")
    
    # Show error count with warning symbol and bold if there are errors
    if metrics["error_count"] > 0:
        discrepant_text = f"Rows Discrepant: {metrics['error_count']}"
        add_str_safe(stdscr, y + 18, x + 2, discrepant_text, curses.A_BOLD)
        add_str_safe(stdscr, y + 18, x + 2 + len(discrepant_text) + 1, "(!) ", curses.color_pair(4))
    else:
        add_str_safe(stdscr, y + 18, x + 2, f"Rows Discrepant: {metrics['error_count']}")

def draw_help(stdscr, y, x, width, state):
    """Draw keyboard controls help split into two lines."""
    # If plan execution is active, show simplified help
    if state.plan_execution_state["active"]:
        current_step = state.plan_execution_state["current_step"]
        total_steps = state.plan_execution_state["total_steps"]
        help_line1 = f"PLAN MODE: Step {current_step+1}/{total_steps} - Press RIGHT ARROW to proceed to next step"
        help_line2 = "Plan will execute operations one at a time. Regular controls disabled until plan completes."
    else:
        # Regular help text
        help_line1 = "i: Insert | u: Update | d: Delete | I/U: Insert/Update w/ NULL | ^i/u: Corrupt Insert/Update"
        help_line2 = "^x: Corrupt Target Score | e: Extract | l: Load | s: Seatbelt | r: Remove Filter | n: Toggle NULL Mismap | q: Quit"
    
    add_str_safe(stdscr, y, x, help_line1[:width])
    add_str_safe(stdscr, y+1, x, help_line2[:width])

def render_ui(stdscr, state, sim_state):
    """Render the UI components."""
    stdscr.clear()

    # Calculate panel dimensions
    max_y, max_x = stdscr.getmaxyx()
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

    draw_source_db(stdscr, 0, source_x, top_height, main_panel_width, state, sim_state)
    draw_pipeline(stdscr, 0, pipeline_x, top_height, main_panel_width, state, sim_state)
    draw_corrupt_filter(stdscr, 0, filter_x, top_height, filter_panel_width, state, sim_state)
    draw_target_db(stdscr, 0, target_x, top_height, main_panel_width, state, sim_state)

    # Draw additional panels
    draw_seatbelt(stdscr, top_height, pipeline_x, main_panel_width, state, sim_state)
    draw_logs(stdscr, top_height + seatbelt_height, 0, log_height, max_x - metrics_width, state)
    draw_metrics(stdscr, top_height + seatbelt_height, max_x - metrics_width, log_height, metrics_width, state, sim_state)

    # Draw help at the bottom (now 2 lines) - pass state to show appropriate help
    draw_help(stdscr, max_y - 2, 0, max_x, state)

    # Display key buffer (if any keys have been pressed)
    if state.key_buffer:
        current_time = time.time()
        # Check if we should clear the buffer due to inactivity
        if current_time - state.last_key_activity > 30:  # 30 seconds timeout
            state.key_buffer.clear()
        else:
            # Pad the buffer with spaces to always show 32 characters
            buffer_display = ''.join(state.key_buffer).ljust(32)
            add_str_safe(stdscr, max_y - 3, 0, buffer_display, curses.color_pair(3))  # Yellow color for visibility

def start_seatbelt_animation(state):
    """Start the seatbelt animation."""
    with state.lock:
        sim_state = state.simulator.get_state()
        
        state.seatbelt_animation_state = {
            "active": True,
            "step": 1,  # Start at step 1 (Reading Source DB Signatures)
            "start_time": time.time(),
            "source_rows_read": len(sim_state['source_db']),
            "target_rows_read": len(sim_state['target_db']),
            "paused_until": time.time() + 0.5,  # Show step 1 for 0.5 second
            "completed": False,
            "new_metrics": {"error_count": 0, "pending_count": 0, "valid_count": 0}
        }

def update_seatbelt_animation(state):
    """Update the seatbelt animation state based on timing."""
    try:
        if not state.seatbelt_animation_state["active"]:
            return

        current_time = time.time()

        # If we're waiting for the next step
        if current_time < state.seatbelt_animation_state["paused_until"]:
            return

        # Advance to the next step
        current_step = state.seatbelt_animation_state["step"]

        if current_step == 1:  # Reading Source DB Signatures -> Reading Target DB Signatures
            state.seatbelt_animation_state["step"] = 2
            state.seatbelt_animation_state["paused_until"] = current_time + 0.5  # Show step 2 for 0.5 second

        elif current_step == 2:  # Reading Target DB Signatures -> Processing
            state.seatbelt_animation_state["step"] = 3
            # Update simulation state
            with state.lock:
                sim_state = state.simulator.get_state()
                metrics = sim_state['metrics']
                state.seatbelt_animation_state["new_metrics"] = {
                    "error_count": metrics['error_count'],
                    "pending_count": metrics['pending_count'],
                    "valid_count": metrics['valid_count'],
                }
            state.seatbelt_animation_state["paused_until"] = current_time + 0.5  # Show step 3 for 0.5 second

        elif current_step == 3:  # Processing -> Update Complete
            state.seatbelt_animation_state["step"] = 4
            state.seatbelt_animation_state["paused_until"] = current_time + 0.5  # Show completion for 0.5 second

        elif current_step == 4:  # Done with animation
            state.seatbelt_animation_state["active"] = False
            state.seatbelt_animation_state["completed"] = True
    except Exception as e:
        logging.error(f"ERROR in update_seatbelt_animation: {str(e)}")
        # Reset animation state to avoid getting stuck
        state.seatbelt_animation_state["active"] = False
        state.seatbelt_animation_state["completed"] = True 

def load_plan_from_file(file_path):
    """Load a test plan from a YAML or JSON file."""
    import yaml
    try:
        with open(file_path, 'r') as f:
            data = yaml.safe_load(f)
            if 'plan' in data and isinstance(data['plan'], list):
                return data['plan']
            else:
                logging.error(f"Invalid plan format in {file_path}: 'plan' key not found or not a list")
                return None
    except Exception as e:
        logging.error(f"Error loading plan from {file_path}: {str(e)}")
        return None

def execute_plan_step(state):
    """Execute the current step in the plan."""
    if not state.plan_execution_state["active"] or state.plan_execution_state["current_step"] >= state.plan_execution_state["total_steps"]:
        return False
    
    # Get the current step
    current_step = state.plan_execution_state["current_step"]
    operation = state.plan_execution_state["plan"][current_step]
    repeat = False
    op_type = operation.get('operation', 'unknown')

    # Execute the operation
    try:
        if op_type == 'initialize':
            # Initialize is a sequence of operations: seatbelt_check, extract, load, seatbelt_check
            start_seatbelt_animation(state)
            state.simulator.seatbelt_check()
            state.simulator.extract()
            state.simulator.load() 
            start_seatbelt_animation(state)
            state.simulator.seatbelt_check()
        elif op_type == 'insert':
            row = operation.get('row')
            state.simulator.insert_row(row)
        elif op_type == 'update':
            row = operation.get('row', {})
            # Extract row_id from the row object itself
            row_id = row.get('id')
            state.simulator.update_row(row_id, row)
        elif op_type == 'delete':
            state.simulator.delete_row()
        elif op_type == 'extract':
            state.simulator.extract()
        elif op_type == 'load':
            state.simulator.load()
        elif op_type == 'seatbelt_check':
            start_seatbelt_animation(state)
            state.simulator.seatbelt_check()
        elif op_type == 'corrupt_by_insert':
            state.simulator.corrupt_by_insert()
        elif op_type == 'corrupt_by_update':
            # Check if a specific row is provided for corruption
            if 'row' in operation:
                row_data = operation['row']
                state.simulator.corrupt_by_update(row_data)
            else:
                state.simulator.corrupt_by_update()
        elif op_type == 'corrupt_by_delete':
            state.simulator.corrupt_by_delete()
        elif op_type == 'insert_with_null':
            column = operation.get('column')
            state.simulator.insert_with_null(column)
        elif op_type == 'update_with_null':
            column = operation.get('column')
            state.simulator.update_with_null(column)
        elif op_type == 'remove_from_filter':
            state.simulator.remove_from_filter()
        elif op_type == 'toggle_null_corruption':
            column = operation.get('column')
            state.simulator.toggle_null_corruption(column)
        elif op_type == 'set_null_corruption':
            column = operation.get('column')
            enabled = operation.get('enabled', True)
            state.simulator.set_null_corruption_for_column(column, enabled)
        elif op_type == 'random':
            state.simulator.random_operation()
        elif op_type == 'corrupt_target':
            # Check if a specific row is provided for corruption
            if 'row' in operation:
                row_data = operation['row'].copy()
                # Extract row_id if specified in the row data
                row_id = row_data.get('id')
                if row_id is not None:
                    state.simulator.corrupt_target_with_row(row_id, row_data)
                else:
                    state.simulator.corrupt_target_score()
            else:
                # Fall back to random corruption
                state.simulator.corrupt_target_score()
        elif op_type == 'expect':
            repeat = True
        else:
            logging.warning(f"Unknown operation type: {op_type}")
    except Exception as e:
        logging.error(f"Error executing plan step: {str(e)}")
    
    # Advance to the next step
    state.plan_execution_state["current_step"] += 1
    
    # Check if we've completed the plan
    if state.plan_execution_state["current_step"] >= state.plan_execution_state["total_steps"]:
        logging.info("Plan execution completed")
        state.plan_execution_state["active"] = False
        return True
    elif repeat:
        return execute_plan_step(state)
    else:
        # Wait for user input before next step
        state.plan_execution_state["waiting_for_input"] = True
        return False

def run_ui_loop(stdscr, state, log_handler):
    """Main UI loop."""
    # Initialize state tracking
    last_redraw_time = 0
    last_source_ops = 0
    last_target_ops = 0
    last_staging_size = 0
    
    # Get terminal dimensions
    max_y, max_x = stdscr.getmaxyx()
    last_terminal_size = (max_y, max_x)
    
    # Main loop
    running = True
    while running:
        current_time = time.time()

        # Check if we need to redraw the screen
        needs_redraw = False

        # Update seatbelt animation if active
        if state.seatbelt_animation_state["active"]:
            old_step = state.seatbelt_animation_state["step"]
            update_seatbelt_animation(state)
            if old_step != state.seatbelt_animation_state["step"]:
                needs_redraw = True

        # Get current dimensions in case window was resized
        max_y, max_x = stdscr.getmaxyx()
        if (max_y, max_x) != last_terminal_size:
            needs_redraw = True
            last_terminal_size = (max_y, max_x)

        # Check if data has changed
        with state.lock:
            sim_state = state.simulator.get_state()
            source_ops = sim_state['metrics']['source_ops_count']
            target_ops = sim_state['metrics']['target_ops_count']
            staging_size = len(sim_state['staging'])

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

                # If in plan execution mode, only allow right arrow to advance
                if state.plan_execution_state["active"]:
                    if key == curses.KEY_RIGHT:
                        if state.plan_execution_state["waiting_for_input"]:
                            state.plan_execution_state["waiting_for_input"] = False
                            
                            # Get the operation type before executing the step
                            current_step = state.plan_execution_state["current_step"]
                            operation = state.plan_execution_state["plan"][current_step]
                            op_type = operation.get('operation', 'unknown')
                            
                            # Execute the plan step
                            execute_plan_step(state)
                            
                            # Add to key buffer - use the key that would normally trigger this operation
                            key_char = {
                                'insert': 'i',
                                'update': 'u',
                                'delete': 'd',
                                'extract': 'e',
                                'load': 'l',
                                'seatbelt_check': 's',
                                'initialize': 'init'
                            }.get(op_type, '?')
                            
                            # Add handlers for new plan step types
                            if op_type == 'corrupt_by_insert': key_char = '^i'
                            elif op_type == 'corrupt_by_update': key_char = '^u'
                            elif op_type == 'corrupt_by_delete': key_char = '^d'
                            elif op_type == 'corrupt_target': key_char = '^x'
                            elif op_type == 'insert_with_null': key_char = 'I'
                            elif op_type == 'update_with_null': key_char = 'U'
                            elif op_type == 'remove_from_filter': key_char = 'r'
                            elif op_type == 'toggle_null_corruption': key_char = 'n'
                            elif op_type == 'set_null_corruption': key_char = 'n' # Use 'n' as well
                            elif op_type == 'random': key_char = '*' # Use * for random

                            state.key_buffer.append(key_char)
                            if len(state.key_buffer) > 32:
                                state.key_buffer.pop(0)
                            state.last_key_activity = time.time()
                    elif key == ord('q'):  # Allow quitting even in plan mode
                        running = False
                else:
                    # Regular key handling when not in plan mode
                    if key == ord('q'):
                        running = False
                    elif key == ord('i'):
                        state.simulator.insert_row()
                        # Add to key buffer
                        state.key_buffer.append('i')
                        if len(state.key_buffer) > 32:
                            state.key_buffer.pop(0)
                        state.last_key_activity = time.time()
                    elif key == ord('u'):
                        if state.simulator.update_row() is not None:
                            # Add to key buffer
                            state.key_buffer.append('u')
                            if len(state.key_buffer) > 32:
                                state.key_buffer.pop(0)
                            state.last_key_activity = time.time()
                    elif key == ord('d'):
                        if state.simulator.delete_row() is not None:
                            # Add to key buffer
                            state.key_buffer.append('d')
                            if len(state.key_buffer) > 32:
                                state.key_buffer.pop(0)
                            state.last_key_activity = time.time()
                    elif key == ord('e'):
                        state.simulator.extract()
                        # Add to key buffer
                        state.key_buffer.append('e')
                        if len(state.key_buffer) > 32:
                            state.key_buffer.pop(0)
                        state.last_key_activity = time.time()
                    elif key == ord('l'):
                        state.simulator.load()
                        # Add to key buffer
                        state.key_buffer.append('l')
                        if len(state.key_buffer) > 32:
                            state.key_buffer.pop(0)
                        state.last_key_activity = time.time()
                    elif key == ord('s'):
                        # Add to key buffer regardless of whether the command executes
                        state.key_buffer.append('s')
                        if len(state.key_buffer) > 32:
                            state.key_buffer.pop(0)
                        state.last_key_activity = time.time()
                        # Run seatbelt check if animation is not active
                        if not state.seatbelt_animation_state["active"]:
                            start_seatbelt_animation(state)
                            state.simulator.seatbelt_check()
                    elif key == 21:  # CTRL-u (21 is the ASCII code for CTRL-u)
                        # Add to key buffer regardless of whether the command executes
                        state.key_buffer.append('^u')  # Use ^u to indicate CTRL-u
                        if len(state.key_buffer) > 32:
                            state.key_buffer.pop(0)
                        state.last_key_activity = time.time()
                        # Run corrupt update if possible
                        state.simulator.corrupt_by_update()
                    elif key == 24:  # CTRL-x 
                        # Add to key buffer regardless of whether the command executes
                        state.key_buffer.append('^x')  # Use ^x to indicate CTRL-x
                        if len(state.key_buffer) > 32:
                            state.key_buffer.pop(0)
                        state.last_key_activity = time.time()
                        # Corrupt a random row in the target DB
                        state.simulator.corrupt_target_score()
                    elif key == 9:  # CTRL-i (9 is the ASCII code for CTRL-i)
                        # Add to key buffer regardless of whether the command executes
                        state.key_buffer.append('^i')  # Use ^i to indicate CTRL-i
                        if len(state.key_buffer) > 32:
                            state.key_buffer.pop(0)
                        state.last_key_activity = time.time()
                        # Run corrupt insert
                        state.simulator.corrupt_by_insert()
                    elif key == ord('r'):
                        # Add to key buffer regardless of whether the command executes
                        state.key_buffer.append('r')
                        if len(state.key_buffer) > 32:
                            state.key_buffer.pop(0)
                        state.last_key_activity = time.time()
                        # Run remove from filter
                        state.simulator.remove_from_filter()
                    elif key == ord('n'):
                        # Add to key buffer regardless of whether the command executes
                        state.key_buffer.append('n')
                        if len(state.key_buffer) > 32:
                            state.key_buffer.pop(0)
                        state.last_key_activity = time.time()
                        # Run toggle NULL corruption
                        state.simulator.toggle_null_corruption()
                    elif key == ord('I'):  # Capital I for NULL insert
                        # Add to key buffer regardless of whether the command executes
                        state.key_buffer.append('I')
                        if len(state.key_buffer) > 32:
                            state.key_buffer.pop(0)
                        state.last_key_activity = time.time()
                        # Run NULL insert
                        state.simulator.insert_with_null()
                    elif key == ord('U'):  # Capital U for NULL update
                        # Add to key buffer regardless of whether the command executes
                        state.key_buffer.append('U')
                        if len(state.key_buffer) > 32:
                            state.key_buffer.pop(0)
                        state.last_key_activity = time.time()
                        # Run NULL update
                        state.simulator.update_with_null()
        except Exception as e:
            # Display any errors that might occur
            state.key_buffer.append('!')  # Add error indicator to buffer
            if len(state.key_buffer) > 10:
                state.key_buffer.pop(0)
            needs_redraw = True

        # Only redraw if necessary
        if needs_redraw:
            # Get latest state from the simulator
            with state.lock:
                sim_state = state.simulator.get_state()
            
            # Get logs from log handler - use a try/except to avoid deadlocks
            try:
                if log_handler.lock.acquire(blocking=False):
                    try:
                        state.logs = log_handler.logs.copy()
                    finally:
                        log_handler.lock.release()
            except Exception:
                pass  # If we can't get logs, just continue with what we have
            
            # Render the UI
            render_ui(stdscr, state, sim_state)
            
            # Refresh screen
            stdscr.refresh()
            last_redraw_time = current_time

        # Sleep to reduce CPU usage
        time.sleep(0.05)  # Smaller delay for responsiveness but not too much CPU

def main(stdscr, args, log_handler):
    """Main function for the TUI."""
    logger = logging.getLogger(__name__)
    logger.info("Initializing Seatbelt Demo TUI")
    
    # Initialize TUI state
    state = TUIState()
    
    # Initialize simulator and potentially load plan
    plan = None
    
    # Use the config file if provided
    if args.config:
        try:
            state.simulator = Simulator.from_config_file(args.config)
            logger.info(f"Loaded custom schema from {args.config}")
            
            # Check if config file includes a plan
            if not args.no_plan:
                # Try to load the plan from the same config file
                import yaml
                with open(args.config, 'r') as f:
                    config_data = yaml.safe_load(f)
                    if 'plan' in config_data and isinstance(config_data['plan'], list):
                        plan = config_data['plan']
                        logger.info(f"Plan loaded from config file with {len(plan)} steps")
                    else:
                        logger.info("No plan found in config file")
        except Exception as e:
            logger.error(f"Error loading configuration file: {e}")
            logger.info("Falling back to default schema")
            state.simulator = Simulator()
    else:
        state.simulator = Simulator()
    
    logger.info("Simulator initialized")
    
    # Setup UI callbacks
    def on_data_changed():
        """Callback for when simulator data changes."""
        with state.lock:
            # Get the latest state from the simulator
            sim_state = state.simulator.get_state()
            
            # Update TUI state from simulator state
            state.last_modified_row_id = sim_state['last_modified_row_id']
            
            # Check if any rows were recently loaded
            current_time = time.time()
            if state.last_load_time != sim_state['sync_state']['last_load_ts']:
                # Update loading timestamp and clear previous highlighting
                state.last_load_time = current_time
                state.recently_loaded_ids.clear()
                
                # Get IDs of rows that were loaded
                for row_id in sim_state['target_db'].keys():
                    if row_id not in state.recently_loaded_ids:
                        state.recently_loaded_ids.add(row_id)
    
    # Set callback
    state.simulator.on_data_changed = on_data_changed
    
    # Initialize curses
    try:
        # Initialize curses settings
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
        
        # If a plan was loaded, start executing it
        if plan and not args.no_plan:
            logger.info(f"Starting plan execution with {len(plan)} steps")
            state.plan_execution_state = {
                "active": True,
                "plan": plan,
                "current_step": 0,
                "total_steps": len(plan),
                "waiting_for_input": False
            }
            # Execute first plan step if first plan step is initialize
            if plan[0]['operation'] == 'initialize':
                execute_plan_step(state)
            state.plan_execution_state["waiting_for_input"] = True
        else:
            # Add some initial data only if a config file with initial_data wasn't used
            sim_state = state.simulator.get_state()
            source_db = sim_state['source_db']
            
            # Only insert sample data if the source database is empty
            if len(source_db) == 0:
                logger.info("Creating initial sample data")
                for _ in range(3):
                    state.simulator.insert_row()
            else:
                logger.info(f"Using existing data from config (found {len(source_db)} rows)")
        
        # If --check-only flag is provided, exit immediately
        if args.check_only:
            logger.info("Check-only mode: initialization successful. Exiting.")
            print("Initialization successful. Exiting.")
            return

        # Run the main UI loop
        logger.info("Starting UI main loop")
        run_ui_loop(stdscr, state, log_handler)
        
    except Exception as e:
        # If a critical error occurs, exit curses mode and print the error
        curses.endwin()
        logger.error(f"Critical error occurred: {str(e)}", exc_info=True)
        print(f"Critical error occurred: {str(e)}")
        import traceback
        traceback.print_exc()

def run_tui():
    """Main entry point for the TUI."""
    # Setup
    args = parse_args()
    
    # Add direct logging to debug file for startup issues
    try:
        with open("tui_debug.log", "w") as f:
            f.write(f"Starting Seatbelt Demo TUI at {datetime.now()}\n")
    except Exception:
        pass  # Continue even if we can't write the debug file
    
    # Setup logging
    logger, log_handler = setup_logging()
    logger.info("Seatbelt Demo TUI started")
    
    # Run the TUI
    try:
        wrapper(lambda stdscr: main(stdscr, args, log_handler))
    except Exception as e:
        print(f"Fatal error: {str(e)}")
        import traceback
        traceback.print_exc()

if __name__ == "__main__":
    run_tui() 