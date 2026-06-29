package row_mappers

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"seatbelt/pkg/seatbelt"
	"seatbelt/pkg/typesystem"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:generate make -C ../.. wasm

// peerdbWasm is the embedded reference mapper (Zig), built from
// wasm-mappers/peerdb/. It implements mapper ABI v1 (see wasm-mappers/ABI.md).
//
//go:embed peerdb.wasm
var peerdbWasm []byte

// abiVersion is the mapper ABI revision this host speaks. A module reporting a
// different abi_version() is rejected.
const abiVersion = 1

// WasmMapper runs row-mapping logic inside a sandboxed WASM module loaded via
// wazero. It satisfies seatbelt.RowMapper. Any module implementing mapper ABI
// v1 can be loaded — NewWasmMapper uses the embedded reference module, while
// NewWasmMapperFromBinary loads a caller-supplied module.
//
// A WasmMapper is created once per table and reused for every row. Calls are
// serialized with a mutex because they share the module's linear memory.
type WasmMapper struct {
	ctx     context.Context
	runtime wazero.Runtime
	mod     api.Module
	mem     api.Memory

	mu        sync.Mutex
	alloc     api.Function
	dealloc   api.Function
	setSchema api.Function
	srcFn     api.Function
	tgtFn     api.Function
	lastError api.Function
}

// NewWasmMapper loads the embedded reference (peerdb) mapper.
func NewWasmMapper(tableDef seatbelt.TableDefinition) (*WasmMapper, error) {
	return NewWasmMapperFromBinary(peerdbWasm, tableDef)
}

// NewWasmMapperFromBinary loads any module implementing mapper ABI v1. This is
// the entry point for third-party mappers compiled to WASM.
func NewWasmMapperFromBinary(binary []byte, tableDef seatbelt.TableDefinition) (*WasmMapper, error) {
	ctx := context.Background()

	rt := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	compiled, err := rt.CompileModule(ctx, binary)
	if err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("wasm compile: %w", err)
	}

	// The reference module is a wasm32-wasi executable that does no I/O; skip
	// its _start (which would call proc_exit).
	cfg := wazero.NewModuleConfig().WithName("seatbelt_mapper").WithStartFunctions()
	mod, err := rt.InstantiateModule(ctx, compiled, cfg)
	if err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("wasm instantiate: %w", err)
	}

	m := &WasmMapper{
		ctx:       ctx,
		runtime:   rt,
		mod:       mod,
		mem:       mod.Memory(),
		alloc:     mod.ExportedFunction("alloc"),
		dealloc:   mod.ExportedFunction("dealloc"),
		setSchema: mod.ExportedFunction("set_schema"),
		srcFn:     mod.ExportedFunction("transform_source"),
		tgtFn:     mod.ExportedFunction("transform_target"),
		lastError: mod.ExportedFunction("last_error"),
	}

	abiFn := mod.ExportedFunction("abi_version")
	for name, fn := range map[string]api.Function{
		"abi_version": abiFn, "alloc": m.alloc, "dealloc": m.dealloc,
		"set_schema": m.setSchema, "transform_source": m.srcFn,
		"transform_target": m.tgtFn, "last_error": m.lastError,
	} {
		if fn == nil {
			m.Close()
			return nil, fmt.Errorf("wasm module missing required export %q", name)
		}
	}

	res, err := abiFn.Call(ctx)
	if err != nil {
		m.Close()
		return nil, fmt.Errorf("abi_version call: %w", err)
	}
	if got := uint32(res[0]); got != abiVersion {
		m.Close()
		return nil, fmt.Errorf("unsupported mapper ABI version %d (host speaks %d)", got, abiVersion)
	}

	if err := m.uploadSchema(tableDef); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}

// Close releases the WASM runtime.
func (m *WasmMapper) Close() {
	if m.mod != nil {
		m.mod.Close(m.ctx)
	}
	if m.runtime != nil {
		m.runtime.Close(m.ctx)
	}
}

// TransformSourceToCommon maps one source row to its common string.
func (m *WasmMapper) TransformSourceToCommon(row []interface{}) (string, error) {
	return m.transform(m.srcFn, row)
}

// TransformTargetToCommon maps one target row to its common string.
func (m *WasmMapper) TransformTargetToCommon(row []interface{}) (string, error) {
	return m.transform(m.tgtFn, row)
}

// ── internals ────────────────────────────────────────────────────────────────

func (m *WasmMapper) uploadSchema(tableDef seatbelt.TableDefinition) error {
	src := tableDef.SourceColumns()
	tgt := tableDef.TargetColumns()

	srcFamilies := make([]string, len(src))
	for i, col := range src {
		srcFamilies[i] = familyString(col.TypeInfo)
	}
	tgtFamilies := make([]string, len(tgt))
	for i, col := range tgt {
		tgtFamilies[i] = familyString(col.TypeInfo)
	}

	payload, err := json.Marshal(struct {
		SourceFamilies []string `json:"source_families"`
		TargetFamilies []string `json:"target_families"`
	}{srcFamilies, tgtFamilies})
	if err != nil {
		return fmt.Errorf("schema marshal: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ptr, err := m.writeInput(payload)
	if err != nil {
		return err
	}
	defer m.free(ptr, len(payload))

	res, err := m.setSchema.Call(m.ctx, uint64(ptr), uint64(len(payload)))
	if err != nil {
		return fmt.Errorf("set_schema call: %w", err)
	}
	if int32(res[0]) != 0 {
		return fmt.Errorf("set_schema failed: %s", m.readError())
	}
	return nil
}

func (m *WasmMapper) transform(fn api.Function, row []interface{}) (string, error) {
	payload, err := json.Marshal(row)
	if err != nil {
		return "", fmt.Errorf("row marshal: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ptr, err := m.writeInput(payload)
	if err != nil {
		return "", err
	}
	defer m.free(ptr, len(payload))

	res, err := fn.Call(m.ctx, uint64(ptr), uint64(len(payload)))
	if err != nil {
		return "", fmt.Errorf("wasm transform call: %w", err)
	}
	packed := res[0]
	if packed == 0 {
		return "", fmt.Errorf("wasm transform failed: %s", m.readError())
	}
	outPtr := uint32(packed >> 32)
	outLen := uint32(packed)
	out, ok := m.mem.Read(outPtr, outLen)
	if !ok {
		return "", fmt.Errorf("wasm output region [%d,%d) out of bounds", outPtr, outLen)
	}
	return string(out), nil // string() copies; out aliases module memory
}

// writeInput allocates a guest region and copies data into it, returning the
// guest pointer. Caller must free it. Caller holds m.mu.
func (m *WasmMapper) writeInput(data []byte) (uint32, error) {
	res, err := m.alloc.Call(m.ctx, uint64(len(data)))
	if err != nil {
		return 0, fmt.Errorf("wasm alloc call: %w", err)
	}
	ptr := uint32(res[0])
	if ptr == 0 {
		return 0, fmt.Errorf("wasm alloc returned null for %d bytes", len(data))
	}
	if !m.mem.Write(ptr, data) {
		m.free(ptr, len(data))
		return 0, fmt.Errorf("wasm write of %d bytes out of bounds", len(data))
	}
	return ptr, nil
}

// free releases a guest region. Caller holds m.mu.
func (m *WasmMapper) free(ptr uint32, length int) {
	_, _ = m.dealloc.Call(m.ctx, uint64(ptr), uint64(length))
}

// readError pulls the module's last_error message. Caller holds m.mu.
func (m *WasmMapper) readError() string {
	res, err := m.lastError.Call(m.ctx)
	if err != nil || res[0] == 0 {
		return "unknown error"
	}
	packed := res[0]
	ptr := uint32(packed >> 32)
	length := uint32(packed)
	msg, ok := m.mem.Read(ptr, length)
	if !ok || len(msg) == 0 {
		return "unknown error"
	}
	return string(msg)
}

func familyString(info *typesystem.DatabaseTypeInfo) string {
	if info == nil {
		return "unknown"
	}
	return string(info.Family)
}
