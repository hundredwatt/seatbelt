#!/usr/bin/env python
"""Run tests with logging enabled."""

import sys
import logging
import unittest
import pytest

if __name__ == "__main__":
    # Configure logging
    logging.basicConfig(
        level=logging.INFO,
        format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
    )
    
    # For more detailed logs, use DEBUG level
    # logging.basicConfig(
    #     level=logging.DEBUG,
    #     format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
    # )
    
    # Set specific loggers
    pyseatbelt_logger = logging.getLogger('pyseatbelt')
    pyseatbelt_logger.setLevel(logging.DEBUG)
    
    # Run tests
    if len(sys.argv) > 1 and sys.argv[1] == '--pytest':
        # Use pytest
        sys.exit(pytest.main(sys.argv[2:]))
    else:
        # Use unittest
        unittest.main(module=None, argv=sys.argv[:1] + sys.argv[1:]) 