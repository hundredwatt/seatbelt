# Concepts: Data Change Validation & Hash Triangulation

Seatbelt validates a live CDC pipeline without ever requiring a clean row-for-row value comparison
between source and destination. This document explains why that constraint exists and the two
primitives that work around it.

## Why you can't just compare hashes

The intuitive test is: hash each source row, hash the corresponding destination row, compare. On a
live pipeline this fails constantly even when replication is perfectly correct:

- **JSON / semi-structured columns.** Serialization doesn't preserve key order, so
  `MD5(json::text)` differs across systems for identical data.
- **Lossy or representational type conversions.** Floating-point truncation, decimal scale,
  timestamp precision, integer widening — the destination's text form legitimately differs from the
  source's.
- **In-flight changes.** The source row changed and the change hasn't propagated yet. A point-in-time
  comparison sees a "mismatch" that will resolve itself momentarily.

So Seatbelt assumes a clean comparison is *impossible* and asks a different question: can we still test
the pipeline comprehensively?

## Primitive 1: Data Change Validation

Instead of comparing values, track **operations**. Each row, identified by primary key, has an
operation on the source side and on the destination side over a time window:

```
DOES_NOT_EXIST · NOOP · INSERT · UPDATE · DELETE
```

The invariant: **every operation on a source row must produce an equivalent operation on the
destination row.** From that, with no per-column configuration, Seatbelt asserts:

- No source rows missing from the destination
- No destination rows missing from the source
- Row counts match, once you account for replication lag
- Every INSERT / UPDATE / DELETE on the source produced a matching change on the destination

It works by hashing each row into a checksum on both sides and watching how those checksums *change*
between iterations — not whether they're equal to each other. A row that goes from one checksum to
another is an UPDATE; a row that appears is an INSERT; one that disappears is a DELETE. Comparing the
source's operation history to the destination's reveals dropped updates, missing rows, phantom rows,
duplicates, and un-replicated deletes.

The exact classification and the failure-detection rules are specified, with a conformance test
suite, in [`../change-validation-core`](../change-validation-core). They're re-implemented in the Go
program and exposed as SQL functions by the DuckDB extension.

### Why "Pending" vs "Error"

A single observation can't distinguish "this row is permanently wrong" from "this change just hasn't
arrived yet." Seatbelt needs to see a row *settle* — change on the source and then, a beat later,
change on the destination. So a discrepancy first shows up as **Pending** (unreconciled) and is only
promoted to **Error** once the operation history proves it's a real failure rather than lag. This is
why a live change stream matters: batch (`--initial-load`) runs can flag rows as Pending but can't
confirm Errors on their own.

## Primitive 2: Hash Triangulation

Data Change Validation tells you operations match, but not that *values* match across all columns. To
get a full audit without a clean cross-system hash, Seatbelt triangulates:

1. Use Data Change Validation to isolate the **static** rows — filtering out live churn and anything
   already flagged.
2. Compute a cheap **source hash** with the fastest native function on the source
   (PostgreSQL `hashtextextended`), keeping source load minimal.
3. Compute a deterministic **destination hash** on the destination.
4. Maintain a map of `source_hash → destination_hash` for every row, built **asynchronously from the
   source change log**, where the pipeline's transformations can be reproduced once.
5. Compare each row's observed `(source_hash, destination_hash)` against the map. A mismatch is a
   validation failure.

The source and destination hashes never need to be equal. The map absorbs every legitimate
transformation (JSON re-serialization, type conversions, etc.), because it's derived from the change
log where the transformation is known. The price is writing a **row mapper** that reproduces those
transformations — see [`02-architecture.md`](./02-architecture.md) and
[`../wasm-mappers`](../wasm-mappers).

## Why minimal source impact

Both primitives extract only **one hash per row** from the source (computed in-database with a native
function), never the row data itself. In the examples, the destination scan reads a few percent of
the table's on-disk size. That's the difference between an audit you can run continuously against a
production database and one you run once a quarter and pray.
