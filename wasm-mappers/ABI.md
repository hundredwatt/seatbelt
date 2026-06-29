# Seatbelt Mapper ABI — v1

A **mapper** turns one database row into a canonical *common string*. Seatbelt
hashes that string on both the source and target sides and compares the hashes to
validate data integrity. The mapping logic runs inside a sandboxed WebAssembly
module so it can be written in any language that compiles to `wasm32` and loaded
at runtime — without rebuilding Seatbelt.

This document is the contract a module must implement to be loadable by the
Seatbelt host (`pkg/row_mappers.NewWasmMapperFromBinary`). The reference
implementation lives in [`peerdb/`](./peerdb) (Zig).

## Module format

- Target `wasm32-wasi` or `wasm32-freestanding`.
- The host instantiates with WASI preview1 available but **does not call `_start`
  or `_initialize`** — do not rely on WASI runtime init, and do no I/O. A module
  built as a WASI executable should leave `_start` as a no-op (the host skips it).
- All ABI functions below must be exported by name.

## Memory model

The module owns its linear memory. The host drives every call as:

1. `ptr = alloc(len)` — host asks the module for a writable region.
2. host writes `len` input bytes at `ptr`.
3. host calls a transform/`set_schema` with `(ptr, len)`.
4. host reads the result, then calls `dealloc(ptr, len)` to release the input.

Transform output lives in module-owned memory and is referenced by a **packed
`u64`**: the high 32 bits are the byte offset, the low 32 bits are the length.

```
out_ptr = packed >> 32
out_len = packed & 0xFFFFFFFF
```

The output region must remain valid until the module's **next** exported call;
the host reads it immediately. The host never deallocates output.

## Exports

| Function | Signature | Description |
|---|---|---|
| `abi_version` | `() -> u32` | Returns `1`. The host rejects any other value. |
| `alloc` | `(len: u32) -> u32` | Allocate `len` bytes; return the offset, or `0` on failure. |
| `dealloc` | `(ptr: u32, len: u32) -> ()` | Free a region previously returned by `alloc`. |
| `set_schema` | `(ptr: u32, len: u32) -> i32` | Load the column schema (JSON, below). Returns `0` on success, `<0` on error (`last_error` set). Called once before any transform. |
| `transform_source` | `(ptr: u32, len: u32) -> u64` | Map one **source** row. Returns the packed output, or `0` on error. |
| `transform_target` | `(ptr: u32, len: u32) -> u64` | Map one **target** row. Returns the packed output, or `0` on error. |
| `last_error` | `() -> u64` | Packed pointer/length of the most recent UTF-8 error message. Valid until the next call. |

## Wire format

### Schema (`set_schema` input)

A JSON object naming the **type family** of each source and target column, in
column order:

```json
{
  "source_families": ["integer", "text", "decimal", "datetime", "json"],
  "target_families": ["integer", "text", "decimal", "datetime", "text"]
}
```

Families currently emitted by the host: `integer`, `float`, `decimal`, `string`,
`boolean`, `datetime`, `date`, `json`, `uuid`, and `unknown` for anything
unmapped. Treat an unrecognized family as a passthrough string.

### Transform input

A JSON array — one element per column, in schema order:

- **Source rows** arrive as JSON strings (or `null`) — this matches the
  replication stream, where every value is delivered as text.
- **Target rows** arrive as native JSON types (number, string, bool, `null`) as
  produced by the target driver.

### Transform output

The **raw** common string — *not* JSON-wrapped, not quoted. The host treats the
returned bytes verbatim as the value to hash. A `null` cell conventionally maps
to `"0"`; see the reference module for the exact per-family rules.

## Versioning

`abi_version` gates compatibility. Any breaking change to the calls, memory
model, or wire format increments it; the host refuses modules whose version it
does not speak.
