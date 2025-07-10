#!/usr/bin/env python3

import argparse
import csv
import difflib
import re
import sys
import textwrap
from dataclasses import dataclass
from typing import Dict, List, Optional, Tuple, Any


# ANSI color codes
class Colors:
    RESET = "\033[0m"
    RED = "\033[91m"
    GREEN = "\033[92m"
    BLUE = "\033[94m"
    YELLOW = "\033[93m"
    CYAN = "\033[96m"
    WHITE = "\033[97m"
    BOLD = "\033[1m"
    UNDERLINE = "\033[4m"
    HEADER = "\033[95m"


@dataclass
class SourceScanRow:
    pk: str
    source_hash: str
    source_text: str


@dataclass
class SourceExtractScanRow:
    pk: str
    source_hash: str
    target_hash: str
    source_text: str
    target_text: str


@dataclass
class TargetScanRow:
    pk: str
    target_hash: str
    target_text: str


def read_source_scan(file_path: str) -> Dict[str, SourceScanRow]:
    rows = {}
    with open(file_path, 'r') as file:
        reader = csv.DictReader(file)
        for row in reader:
            rows[row['pk']] = SourceScanRow(
                pk=row['pk'],
                source_hash=row['source_hash'],
                source_text=row['source_text']
            )
    return rows


def read_source_extract_scan(file_path: str) -> Dict[str, SourceExtractScanRow]:
    rows = {}
    with open(file_path, 'r') as file:
        reader = csv.DictReader(file)
        for row in reader:
            rows[row['pk']] = SourceExtractScanRow(
                pk=row['pk'],
                source_hash=row['source_hash'],
                target_hash=row['target_hash'],
                source_text=row['source_text'],
                target_text=row['target_text']
            )
    return rows


def read_target_scan(file_path: str) -> Dict[str, TargetScanRow]:
    rows = {}
    with open(file_path, 'r') as file:
        reader = csv.DictReader(file)
        for row in reader:
            rows[row['pk']] = TargetScanRow(
                pk=row['pk'],
                target_hash=row['target_hash'],
                target_text=row['target_text']
            )
    return rows


def format_source_row(row: SourceScanRow) -> Dict[str, Any]:
    return {
        "pk": row.pk,
        "source_hash": row.source_hash,
        "source_text": row.source_text
    }


def format_source_extract_source_part(row: SourceExtractScanRow) -> Dict[str, Any]:
    return {
        "pk": row.pk,
        "source_hash": row.source_hash,
        "source_text": row.source_text
    }


def format_target_row(row: TargetScanRow) -> Dict[str, Any]:
    return {
        "pk": row.pk,
        "target_hash": row.target_hash,
        "target_text": row.target_text
    }


def format_source_extract_target_part(row: SourceExtractScanRow) -> Dict[str, Any]:
    return {
        "pk": row.pk,
        "target_hash": row.target_hash,
        "target_text": row.target_text
    }


def compare_dicts(a: Dict[str, Any], b: Dict[str, Any]) -> Dict[str, Tuple[Optional[str], Optional[str]]]:
    """Compare two dictionaries and return differences."""
    all_keys = set(a.keys()) | set(b.keys())
    result = {}
    
    for key in all_keys:
        a_val = a.get(key)
        b_val = b.get(key)
        
        if a_val != b_val:
            result[key] = (a_val, b_val)
    
    return result


def show_text_diff(a_text: str, b_text: str, context: int = 2) -> None:
    """Show a readable diff for large text strings with context lines."""
    if a_text == b_text:
        print(f"{Colors.WHITE}Texts are identical{Colors.RESET}")
        return

    # Using unified_diff for better readability with large texts
    a_lines = a_text.splitlines()
    b_lines = b_text.splitlines()
    
    if not a_lines:
        a_lines = [a_text]
    if not b_lines:
        b_lines = [b_text]
    
    diff = difflib.unified_diff(
        a_lines, 
        b_lines, 
        lineterm='', 
        n=context
    )
    
    for line in diff:
        if line.startswith('+'):
            print(f"{Colors.GREEN}{line}{Colors.RESET}")
        elif line.startswith('-'):
            print(f"{Colors.RED}{line}{Colors.RESET}")
        elif line.startswith('@@'):
            print(f"{Colors.CYAN}{line}{Colors.RESET}")
        else:
            print(line)


def show_diff(a: Dict[str, Any], b: Dict[str, Any], a_label: str, b_label: str) -> bool:
    """Show the differences between two dictionaries and return True if there are differences."""
    differences = compare_dicts(a, b)
    
    if not differences:
        print(f"{Colors.WHITE}No differences between {a_label} and {b_label}.{Colors.RESET}")
        return False
    
    print(f"{Colors.YELLOW}Differences between {a_label} and {b_label}:{Colors.RESET}")
    
    # Handle special cases for different field types
    for key, (a_val, b_val) in differences.items():
        print(f"{Colors.CYAN}Field: {key}{Colors.RESET}")
        
        # For large text fields, show a more readable diff
        if key.endswith('_text') and isinstance(a_val, str) and isinstance(b_val, str) and (len(a_val) > 100 or len(b_val) > 100):
            print(f"{Colors.BOLD}Text Diff:{Colors.RESET}")
            show_text_diff(a_val, b_val)
        else:
            # For other fields or shorter text
            # Find the field with the longer label to align output
            label_max_length = max(len(a_label), len(b_label))
            
            a_val_str = str(a_val) if a_val is not None else "[MISSING]"
            b_val_str = str(b_val) if b_val is not None else "[MISSING]"
            
            # Check if this might be an email with timestamp (common pattern in target_text)
            # Format: various patterns followed by a timestamp
            if key.endswith('_text') and isinstance(a_val, str) and isinstance(b_val, str):
                # Try to find common prefix between strings to separate from the differing part
                # This approach works better than a rigid regex when formats may vary
                common_length = 0
                min_length = min(len(a_val_str), len(b_val_str))
                
                # Find where the strings start to differ
                for i in range(min_length):
                    if a_val_str[i] == b_val_str[i]:
                        common_length += 1
                    else:
                        break
                
                # If we've found a reasonable common prefix (at least 20 chars)
                # and there are differing parts at the end (like timestamps)
                if common_length > 20 and common_length < min_length:
                    a_common = a_val_str[:common_length]
                    b_common = b_val_str[:common_length]
                    a_timestamp = a_val_str[common_length:]
                    b_timestamp = b_val_str[common_length:]
                    
                    print(f"  {a_label.ljust(label_max_length)}: {a_common}{Colors.RED}{a_timestamp}{Colors.RESET}")
                    print(f"  {b_label.ljust(label_max_length)}: {b_common}{Colors.GREEN}{b_timestamp}{Colors.RESET}")
                else:
                    print(f"  {a_label.ljust(label_max_length)}: {Colors.RED}{a_val_str}{Colors.RESET}")
                    print(f"  {b_label.ljust(label_max_length)}: {Colors.GREEN}{b_val_str}{Colors.RESET}")
            else:
                print(f"  {a_label.ljust(label_max_length)}: {Colors.RED}{a_val_str}{Colors.RESET}")
                print(f"  {b_label.ljust(label_max_length)}: {Colors.GREEN}{b_val_str}{Colors.RESET}")
    
    print()
    return True


def parse_inspect_results(text: str) -> Tuple[Optional[str], Optional[str], Optional[str]]:
    """Parse the inspect results text to extract file paths."""
    source_scan_pattern = r"Source inspect scan file file=(.+\.csv)"
    source_extract_pattern = r"Source inspect extract scan file file=(.+\.csv)" 
    target_scan_pattern = r"Target inspect scan file file=(.+\.csv)"
    
    source_scan_match = re.search(source_scan_pattern, text)
    source_extract_match = re.search(source_extract_pattern, text)
    target_scan_match = re.search(target_scan_pattern, text)
    
    source_scan_file = source_scan_match.group(1) if source_scan_match else None
    source_extract_file = source_extract_match.group(1) if source_extract_match else None
    target_scan_file = target_scan_match.group(1) if target_scan_match else None
    
    return source_scan_file, source_extract_file, target_scan_file


def main():
    parser = argparse.ArgumentParser(description='Compare seatbelt inspection files')
    parser.add_argument('--source-scan', help='Path to source scan file')
    parser.add_argument('--source-extract-scan', help='Path to source extract scan file')
    parser.add_argument('--target-scan', help='Path to target scan file')
    args = parser.parse_args()

    source_scan_file = args.source_scan
    source_extract_file = args.source_extract_scan
    target_scan_file = args.target_scan

    # If no command-line arguments provided, try to parse from stdin
    if not (source_scan_file and source_extract_file and target_scan_file):
        print(f"{Colors.CYAN}No file paths provided via command line. Trying to parse from stdin...{Colors.RESET}")
        
        # Read from stdin if available
        if not sys.stdin.isatty():
            input_text = sys.stdin.read()
            source_scan_file, source_extract_file, target_scan_file = parse_inspect_results(input_text)
            
            if not (source_scan_file and source_extract_file and target_scan_file):
                print(f"{Colors.RED}Error: Could not parse all required file paths from stdin.{Colors.RESET}")
                print("Please provide file paths either via command-line arguments or by piping inspect results.")
                sys.exit(1)
            
            print(f"{Colors.GREEN}Found source scan file: {source_scan_file}{Colors.RESET}")
            print(f"{Colors.GREEN}Found source extract scan file: {source_extract_file}{Colors.RESET}")
            print(f"{Colors.GREEN}Found target scan file: {target_scan_file}{Colors.RESET}")
        else:
            parser.print_help()
            sys.exit(1)

    # Read all files
    try:
        source_scan_rows = read_source_scan(source_scan_file)
        source_extract_scan_rows = read_source_extract_scan(source_extract_file)
        target_scan_rows = read_target_scan(target_scan_file)
    except Exception as e:
        print(f"{Colors.RED}Error reading files: {str(e)}{Colors.RESET}")
        sys.exit(1)

    # Get all unique primary keys
    all_pks = set(source_scan_rows.keys()) | set(source_extract_scan_rows.keys()) | set(target_scan_rows.keys())

    if not all_pks:
        print(f"{Colors.YELLOW}Warning: No primary keys found in any of the files.{Colors.RESET}")
        sys.exit(0)

    # Process each primary key
    for pk in sorted(all_pks, key=int):
        print(f"{Colors.HEADER}{Colors.BOLD}===== Primary Key: {pk} ====={Colors.RESET}")
        
        # 1. Compare source scan with source extract scan (source part)
        if pk in source_scan_rows and pk in source_extract_scan_rows:
            source_row = source_scan_rows[pk]
            extract_row_source_part = source_extract_scan_rows[pk]
            
            source_dict = format_source_row(source_row)
            extract_source_dict = format_source_extract_source_part(extract_row_source_part)
            
            show_diff(source_dict, extract_source_dict, "source scan", "source extract scan (source part)")
        elif pk in source_scan_rows:
            print(f"{Colors.RED}Source scan has PK {pk} but it's missing from source extract scan{Colors.RESET}")
        elif pk in source_extract_scan_rows:
            print(f"{Colors.RED}Source extract scan has PK {pk} but it's missing from source scan{Colors.RESET}")
        
        # 2. Compare target scan with source extract scan (target part)
        if pk in target_scan_rows and pk in source_extract_scan_rows:
            target_row = target_scan_rows[pk]
            extract_row_target_part = source_extract_scan_rows[pk]
            
            target_dict = format_target_row(target_row)
            extract_target_dict = format_source_extract_target_part(extract_row_target_part)
            
            show_diff(target_dict, extract_target_dict, "target scan", "source extract scan (target part)")
        elif pk in target_scan_rows:
            print(f"{Colors.RED}Target scan has PK {pk} but it's missing from source extract scan{Colors.RESET}")
        elif pk in source_extract_scan_rows:
            print(f"{Colors.RED}Source extract scan has PK {pk} but it's missing from target scan{Colors.RESET}")
        
        print()


if __name__ == '__main__':
    main() 