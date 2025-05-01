"""Validation operations for the Seatbelt Demo simulator."""

import os
from datetime import datetime
from typing import Any, Dict, List

from pyseatbelt import Source, Target, ValidationEngine


SOURCE_EXTRACT_SCAN_FILE = "data/seatbelt-extract-scan-public.data_proof_test-3447664090.csv"
SOURCE_SCAN_FILE = "data/seatbelt-scan-public.data_proof_test-778689522.csv"
TARGET_SCAN_FILE = "data/seatbelt-clickhouse-scan-peerdb.public_data_proof-1115524834.csv"

class DataSource(Source):
    """A source implementation for the simulator."""
    
    def __init__(self ):
        pass
        
    def read_change_log_changes(self, column_names: List[str]) -> Dict[Any, tuple]:
        """Read changes from the change log.
        
        Args:
            column_names: List of column names to include in the change log
            
        Returns:
            Dictionary mapping row IDs to tuples of (source_hash, target_hash)
        """
        result = {}
        
        with open(SOURCE_EXTRACT_SCAN_FILE, 'r') as file:
            for line in file:
                parts = line.strip().split(',')
                pk = int(parts[0])
                source_hash = int(parts[1])
                target_hash = int(parts[2])
                result[pk] = (source_hash, target_hash)
                    
        return result

    
    def read_signatures(self, column_names: List[str]) -> Dict[Any, Any]:
        result = {}
        
        with open(SOURCE_SCAN_FILE, 'r') as file:
            for line in file:
                parts = line.strip().split(',')
                pk = int(parts[0])
                source_hash = int(parts[1])
                result[pk] = source_hash
                    
        return result
       

class DataTarget(Target):
    """A target implementation for the simulator."""
    
    def __init__(self):
        pass
        
    def read_signatures(self, column_names: List[str]) -> Dict[Any, Any]:
        result = {}

        with open(TARGET_SCAN_FILE, 'r') as file:
            for line in file:
                parts = line.strip().split(',')
                pk = int(parts[0])
                target_hash = int(parts[1])
                result[pk] = target_hash

        return result

if __name__ == "__main__":
    source = DataSource()
    target = DataTarget()

    print("First Run:")
    engine = ValidationEngine()
    metrics = engine.seatbelt_check(source, target)
    for key, value in metrics.items():
        print(key, value)
    print("\n" + ("-" * 80) + "\n")

    os.makedirs("tmp", exist_ok=True)
    shadow_path = "tmp/seatbelt-data-%s" % datetime.now().strftime("%Y%m%d-%H%M%S")
    engine.save_shadow(shadow_path)
    print("Seatbelt data saved to %s" % shadow_path)

    print("Second Run:")
    engine = ValidationEngine(shadow_file=shadow_path)
    metrics = engine.seatbelt_check(source, target)
    for key, value in metrics.items():
        print(key, value)
    second_shadow_path = shadow_path + "-2"
    engine.save_shadow(second_shadow_path)
    print("Seatbelt data saved to %s" % second_shadow_path)
