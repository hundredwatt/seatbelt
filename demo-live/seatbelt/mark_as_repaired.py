#!/usr/bin/env python3
"""
Seatbelt script for marking records as repaired.
This script loads the most recent seatbelt file, updates validation_error=False for specified IDs,
and saves a new seatbelt file.
"""

import os
import sys
import logging
from datetime import datetime
from pathlib import Path

# Add pyseatbelt to the path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', '..', 'pyseatbelt'))

# Import pyseatbelt classes
from pyseatbelt.validation import ValidationEngine

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


def main():
    """Repair validation errors for specified IDs."""
    # Get IDs from environment variable
    ids_env = os.environ.get('IDS')
    if not ids_env:
        logger.error("No IDs provided. Set the IDS environment variable with comma-separated IDs.")
        return 1
    
    # Parse IDs
    try:
        ids = [int(id_str.strip()) for id_str in ids_env.split(',')]
        if not ids:
            logger.error("No valid IDs found in the IDS environment variable.")
            return 1
        logger.info(f"Processing repair for IDs: {ids}")
    except ValueError:
        logger.error("Invalid ID format. IDs must be comma-separated integers.")
        return 1
    
    # Create shadow directory if it doesn't exist
    shadow_dir = Path("data/seatbelt")
    shadow_dir.mkdir(parents=True, exist_ok=True)
    
    # Find the most recent seatbelt file
    previous_shadow_files = list(shadow_dir.glob("seatbelt_*.json"))
    previous_shadow_file = max(previous_shadow_files, default=None, key=lambda x: x.stat().st_mtime)
    
    if not previous_shadow_file:
        logger.error("No existing seatbelt file found. Run check.py first.")
        return 1
    
    logger.info(f"Loading previous seatbelt file")
    engine = ValidationEngine(shadow_file=str(previous_shadow_file))
    
    # Update the shadow by marking the specified IDs as repaired
    repaired_count = 0
    no_error_count = 0
    
    for id in ids:
        if id in engine.shadow:
            if engine.shadow[id]['validation_error'] == True:
                logger.info(f"Marking ID {id} as repaired")
                engine.shadow[id]['validation_error'] = False
                repaired_count += 1
            else:
                logger.warning(f"ID {id} does not have a validation error")
                no_error_count += 1
        else:
            logger.warning(f"ID {id} not found in seatbelt file")
    
    # Generate new shadow file path with timestamp
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    new_shadow_file = shadow_dir / f"seatbelt_{timestamp}.json"
    
    # Save the updated shadow
    engine.save_shadow(str(new_shadow_file))
    logger.info(f"Updated seatbelt data saved")
    logger.info(f"Repair summary: {repaired_count} records repaired, {no_error_count} records without errors")
    
    return 0


if __name__ == "__main__":
    sys.exit(main())
