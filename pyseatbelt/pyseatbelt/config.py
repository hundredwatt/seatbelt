import logging
import os

# Global tracing IDs list
TRACING_IDS = []

class ConfigurationError(Exception):
    """Exception raised for configuration errors."""
    pass

def load_tracing_ids_from_env():
    """Load tracing IDs from TRACING_IDS environment variable."""
    global TRACING_IDS
    tracing_ids_env = os.environ.get('TRACING_IDS', '')
    if tracing_ids_env:
        try:
            # Parse comma-separated list of IDs
            parsed = [int(id_str.strip()) for id_str in tracing_ids_env.split(',') if id_str.strip()]
            for id in parsed:
                TRACING_IDS.append(id)
            logging.info(f"Loaded tracing IDs from environment: {TRACING_IDS}")
        except ValueError as e:
            logging.warning(f"Error parsing TRACING_IDS environment variable: {e}")
            raise ConfigurationError(f"Error parsing TRACING_IDS environment variable: {e}")
    return TRACING_IDS
