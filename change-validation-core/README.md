# change-validation-core

This directory is the **executable reference specification** for Seatbelt's Data Change Validation.

The logic here is small, pure, and dependency-light on purpose: it's the source of truth that the
performance-oriented ports re-implement. If the behavior of validation is ever in question, this
Python is the answer, and `validation_logic_tests.json` is the cross-language conformance suite that
keeps every port honest.

| Port | Location |
|------|----------|
| Reference (Python) | [`validation_logic.py`](./validation_logic.py) |
| Go | `seatbelt/pkg/...` (consumed by the shadow/triangulation flow) |
| SQL (DuckDB) | `duckdb-seatbelt-extension/src/seatbelt_duckdb_extension.cpp` |

## The model

Seatbelt never compares row *values* directly. It tracks, per primary key and per validation
iteration, the **operation** that happened on each side of the pipeline:

```
DOES_NOT_EXIST · NOOP · INSERT · UPDATE · DELETE
INSERT_AND_UPDATE · UPDATE_AND_DELETE · TRANSIENT_UPDATE   (destination-only composites)
```

Two functions derive these operations:

- **`determine_source_operation(checksum_1, checksum_0)`** — compares a row's checksum now
  (`checksum_1`) vs. the previous iteration (`checksum_0`) to classify the source-side operation.
  `None` means the row was absent.
- **`determine_destination_operation(present_end, updated, present_start)`** — classifies the
  destination-side operation from three booleans observed over the iteration. This is where the
  composite operations come from: a row that appeared *and* changed within one window is
  `INSERT_AND_UPDATE`; one that appeared and vanished is `TRANSIENT_UPDATE`; and so on.

A third function supports the Hash Triangulation overlay:

- **`verify_row_integrity_from_incremental_checksums(...)`** — for a row that has gone quiet, confirms
  the last-seen incremental `(source, destination)` checksums still match the current ones. Returns
  `True` (verified) when there's nothing to check.

## The failure-detection rules

**`check_for_validation_error(...)`** is the heart of it. Given the current and previous operations on
both sides (plus whether a validation error already existed, and an optional row-integrity result), it
returns `True` when the pipeline has demonstrably failed. The rules map 1:1 to concrete pipeline bugs:

1. **Stalled propagation** — a non-DELETE source change happened last iteration but the destination
   never moved. (TRANSIENT_UPDATEs at the destination suppress this rule.)
2. **Un-replicated delete** — a source DELETE didn't reach the destination.
3. **Missing in destination** — a row exists/persists in the source but does not exist in the
   destination.
4. **Phantom in destination** — a row exists in the destination but not in the source.
5. **Destination corruption (change-based)** — the destination changed while the source held steady.
6. **Row corruption (checksum-based)** — the source held steady but the incremental row-integrity
   check failed (Hash Triangulation caught a value mismatch).
7. **Sticky error** — a previously detected error persists while both sides are quiet.

These are exactly the assertions described in the top-level README: missing rows, extra rows, missing
updates, un-replicated deletes, and (with triangulation) value-level corruption — all derived from
operations rather than from comparing values.

## Running the conformance tests

Requires Python 3.10+ (the operation classifier uses `match`).

```bash
pip install colorama
python validation_logic.py
# or, without managing a Python install:
uv run --python 3.12 --with colorama python validation_logic.py
```

This runs every case in `validation_logic_tests.json` against the reference functions. The same JSON
drives the Go and DuckDB tests, so a change to the spec means updating one file and re-running all
three ports.
