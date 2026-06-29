package row_mappers

import "testing"

// TestWasmMapperMatchesGolden verifies the embedded reference module reproduces
// the frozen native outputs byte-for-byte.
func TestWasmMapperMatchesGolden(t *testing.T) {
	m, err := NewWasmMapper(benchTableDef)
	if err != nil {
		t.Fatalf("NewWasmMapper: %v", err)
	}
	defer m.Close()

	for i := range benchSourceRows {
		got, err := m.TransformSourceToCommon(benchSourceRows[i])
		if err != nil {
			t.Fatalf("source row %d: %v", i, err)
		}
		if got != goldenSource[i] {
			t.Errorf("source row %d:\n  want=%q\n   got=%q", i, goldenSource[i], got)
		}
	}

	for i := range benchTargetRows {
		got, err := m.TransformTargetToCommon(benchTargetRows[i])
		if err != nil {
			t.Fatalf("target row %d: %v", i, err)
		}
		if got != goldenTarget[i] {
			t.Errorf("target row %d:\n  want=%q\n   got=%q", i, goldenTarget[i], got)
		}
	}
}

// TestWasmMapperFromBinary proves a caller-supplied ABI-v1 module loads through
// the third-party entry point (here, the same embedded bytes).
func TestWasmMapperFromBinary(t *testing.T) {
	m, err := NewWasmMapperFromBinary(peerdbWasm, benchTableDef)
	if err != nil {
		t.Fatalf("NewWasmMapperFromBinary: %v", err)
	}
	defer m.Close()

	got, err := m.TransformSourceToCommon(benchSourceRows[0])
	if err != nil {
		t.Fatal(err)
	}
	if got != goldenSource[0] {
		t.Errorf("want=%q got=%q", goldenSource[0], got)
	}
}

// TestWasmMapperRejectsBadModule ensures non-WASM bytes fail to load.
func TestWasmMapperRejectsBadModule(t *testing.T) {
	if _, err := NewWasmMapperFromBinary([]byte("not wasm"), benchTableDef); err == nil {
		t.Fatal("expected error loading invalid module, got nil")
	}
}

func BenchmarkWasmTransformSource(b *testing.B) {
	m, err := NewWasmMapper(benchTableDef)
	if err != nil {
		b.Fatalf("NewWasmMapper: %v", err)
	}
	defer m.Close()
	b.ResetTimer()
	for i := range b.N {
		if _, err := m.TransformSourceToCommon(benchSourceRows[i%5]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWasmTransformTarget(b *testing.B) {
	m, err := NewWasmMapper(benchTableDef)
	if err != nil {
		b.Fatalf("NewWasmMapper: %v", err)
	}
	defer m.Close()
	b.ResetTimer()
	for i := range b.N {
		if _, err := m.TransformTargetToCommon(benchTargetRows[i%5]); err != nil {
			b.Fatal(err)
		}
	}
}
