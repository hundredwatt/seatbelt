#!/bin/bash

export TARGET_SCAN_FILE=tmp/shadow-bench/seatbelt-clickhouse-scan-peerdb.public_dataset3gb-3072434336.csv
export SOURCE_EXTRACT_FILE=tmp/shadow-bench/seatbelt-extract-scan-public.dataset3gb-3377380690.csv
export SOURCE_SCAN_FILE=tmp/shadow-bench/seatbelt-scan-public.dataset3gb-922987778.csv

# Prompt for sudo password upfront to avoid interruption during benchmarking
sudo -v

hyperfine --warmup 2 --runs 5 \
--setup 'make build' \
--prepare 'sudo -n purge' \
--command-name 'seatbelt-archive-6ab9d5d-86aa003ce6d1 shadow ...' \
'./build/seatbelt-archive-6ab9d5d-86aa003ce6d1 shadow \
    --source-changes $SOURCE_EXTRACT_FILE \
    --source-scan $SOURCE_SCAN_FILE \
    --target-scan $TARGET_SCAN_FILE' \
--command-name 'seatbelt shadow ...' \
'./build/seatbelt shadow \
    --source-changes $SOURCE_EXTRACT_FILE \
    --source-scan $SOURCE_SCAN_FILE \
    --target-scan $TARGET_SCAN_FILE'



