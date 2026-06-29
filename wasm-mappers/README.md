# wasm-mappers

Sandboxed, language-agnostic **row mappers** for Seatbelt. A mapper converts a
database row into a canonical *common string* that Seatbelt hashes and compares
across systems. Mappers compile to WebAssembly and load at runtime, so you can
write your own — in any language that targets `wasm32` — without forking or
rebuilding Seatbelt.

## Layout

```
wasm-mappers/
  ABI.md       # the contract every mapper implements (v1)
  README.md    # this file
  peerdb/      # reference mapper (Zig) — the canonical PeerDB → ClickHouse logic
```

## Using a mapper

The Go host lives in `seatbelt/pkg/row_mappers`:

- `NewWasmMapper(tableDef)` — loads the embedded reference (`peerdb`) module.
- `NewWasmMapperFromBinary(wasmBytes, tableDef)` — loads **your** compiled module.

Both return a `*WasmMapper` satisfying `seatbelt.RowMapper`
(`TransformSourceToCommon` / `TransformTargetToCommon`).

## Writing your own

1. Read [`ABI.md`](./ABI.md) — it defines the exports (`abi_version`, `alloc`,
   `dealloc`, `set_schema`, `transform_source`, `transform_target`,
   `last_error`), the memory model, and the JSON wire format.
2. Implement those exports in your language and compile to `wasm32-wasi` (or
   `wasm32-freestanding`). Do no I/O; the host does not call `_start`.
3. Load the `.wasm` via `NewWasmMapperFromBinary`. Verify byte-for-byte parity
   against the reference for your column types before relying on it.

[`peerdb/`](./peerdb) is a complete, readable example to copy from.

## Building the reference module

Requires **Zig 0.16+**.

```sh
cd peerdb && zig build      # -> peerdb/zig-out/bin/peerdb.wasm
```

The Seatbelt build vendors this artifact into the Go package for embedding:

```sh
cd ../seatbelt && make wasm   # builds + copies peerdb.wasm into pkg/row_mappers/
# equivalently: go generate ./pkg/row_mappers/
```

## Performance note

Running the mapper in WASM is ~2–3× slower than equivalent native Go. That cost
buys sandboxing and runtime pluggability — the ability for third parties to ship
mapping logic in any language without touching the Seatbelt binary. That trade is
the whole point of this design.
