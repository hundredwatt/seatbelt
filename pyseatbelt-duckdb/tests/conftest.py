import logging
import pytest

@pytest.fixture(scope="session", autouse=True)
def configure_logging():
    """Configure logging for tests."""
    # Set up basic configuration at the INFO level
    logging.basicConfig(
        level=logging.INFO,
        format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
    )
    
    # You can also set specific loggers to different levels if needed
    # For example:
    # logging.getLogger('pyseatbelt').setLevel(logging.DEBUG)
    
    # Return the logger in case tests want to use it directly
    return logging.getLogger('pyseatbelt') 