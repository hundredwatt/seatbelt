package seatbelt_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"seatbelt/pkg/seatbelt"

	"github.com/stretchr/testify/assert"
)

func TestUpdateShadow_EmptyAndSubsequent(t *testing.T) {
	sourcePath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "seatbelt-scan-public.data_proof_test-778689522.csv")
	targetPath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "seatbelt-clickhouse-scan-peerdb.public_data_proof-1115524834.csv")
	sourceExtractPath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "seatbelt-extract-scan-public.data_proof_test-3447664090.csv")

	data_files, cleanup := buildDataFileSet(t, sourcePath, targetPath, "", sourceExtractPath) // sourceExtractPath used for SourceChanges
	defer cleanup()
	// Adjusting the DataFileSet for this specific test case as SourceChanges comes from sourceExtractPath
	if data_files.SourceExtractScan != nil && sourceExtractPath != "" {
		// This test uses sourceExtractPath for SourceChanges, not SourceExtractScan
		// So, if sourceExtractPath was used to populate SourceExtractScan in buildDataFileSet,
		// we move it to SourceChanges and nil out SourceExtractScan.
		// This is a bit of a hack due to the specific setup of this test.
		// A more robust solution might involve more specific parameters in buildDataFileSet or a different helper.
		tempFile := data_files.SourceExtractScan.File
		data_files.SourceChanges = &seatbelt.DataFile{File: tempFile}
		data_files.SourceExtractScan = nil
	}

	shadow_file, err := os.CreateTemp("", "shadow.dat")
	if err != nil {
		t.Fatalf("Failed to create shadow file: %v", err)
	}
	defer os.Remove(shadow_file.Name())

	cfg := &seatbelt.Config{
		ShadowPath: shadow_file.Name(),
	}
	os.Remove(shadow_file.Name())

	metrics, err := seatbelt.UpdateShadow(context.Background(), cfg, data_files)
	if err != nil {
		t.Fatalf("Failed to update shadow: %v", err)
	}

	assert.Equal(t, int64(25), metrics.SourceSize)
	assert.Equal(t, int64(25), metrics.TargetSize)
	assert.Equal(t, int64(25), metrics.SeatbeltSize)
	assert.Equal(t, int64(23), metrics.ValidCount)
	assert.Equal(t, int64(2), metrics.PendingCount)
	assert.Equal(t, int64(0), metrics.ErrorCount)

	metrics, err = seatbelt.UpdateShadow(context.Background(), cfg, data_files)
	if err != nil {
		t.Fatalf("Failed to update shadow: %v", err)
	}

	assert.Equal(t, int64(25), metrics.SourceSize)
	assert.Equal(t, int64(25), metrics.TargetSize)
	assert.Equal(t, int64(25), metrics.SeatbeltSize)
	assert.Equal(t, int64(23), metrics.ValidCount)
	assert.Equal(t, int64(0), metrics.PendingCount)
	assert.Equal(t, int64(2), metrics.ErrorCount)
}
func TestUpdateShadow_InitialLoad(t *testing.T) {
	targetPath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "seatbelt-clickhouse-scan-peerdb.public_data_proof-1115524834.csv")
	sourceExtractPath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "seatbelt-extract-scan-public.data_proof_test-3447664090.csv")

	data_files, cleanup := buildDataFileSet(t, "", targetPath, sourceExtractPath, "")
	defer cleanup()

	shadow_file, err := os.CreateTemp("", "shadow_initial.dat")
	if err != nil {
		t.Fatalf("Failed to create shadow file: %v", err)
	}
	defer os.Remove(shadow_file.Name())

	cfg := &seatbelt.Config{
		ShadowPath:  shadow_file.Name(),
		InitialLoad: true, // Set initial load flag
	}
	os.Remove(shadow_file.Name())

	metrics, err := seatbelt.UpdateShadow(context.Background(), cfg, data_files)
	if err != nil {
		t.Fatalf("Failed to update shadow with initial load: %v", err)
	}

	// With initial load, we expect all rows to be valid since we don't check validation errors
	assert.Equal(t, int64(25), metrics.SourceSize)
	assert.Equal(t, int64(25), metrics.TargetSize)
	assert.Equal(t, int64(25), metrics.SeatbeltSize)
	assert.Equal(t, int64(23), metrics.ValidCount)
	assert.Equal(t, int64(2), metrics.PendingCount)
	assert.Equal(t, int64(0), metrics.ErrorCount)
}

func TestUpdateShadow_DuplicateRows(t *testing.T) {
	sourcePath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "source-scan-duplicate-rows.csv")
	targetPath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "target-scan-duplicate-rows.csv")
	sourceExtractPath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "source-extract-duplicate-rows.csv")

	data_files, cleanup := buildDataFileSet(t, "", targetPath, sourceExtractPath, "")
	defer cleanup()

	shadow_file, err := os.CreateTemp("", "shadow_initial.dat")
	if err != nil {
		t.Fatalf("Failed to create shadow file: %v", err)
	}
	defer os.Remove(shadow_file.Name())

	cfg := &seatbelt.Config{
		ShadowPath:  shadow_file.Name(),
		InitialLoad: true, // Set initial load flag
	}
	os.Remove(shadow_file.Name())

	// Initial load
	metrics, err := seatbelt.UpdateShadow(context.Background(), cfg, data_files)
	if err != nil {
		t.Fatalf("Failed to update shadow with initial load: %v", err)
	}

	assert.Equal(t, int64(3), metrics.SourceSize)
	assert.Equal(t, int64(3), metrics.TargetSize)
	assert.Equal(t, int64(3), metrics.SeatbeltSize)
	assert.Equal(t, int64(1), metrics.ValidCount)
	assert.Equal(t, int64(0), metrics.PendingCount)
	assert.Equal(t, int64(2), metrics.ErrorCount)

	// Subsequent load
	data_files, cleanup = buildDataFileSet(t, sourcePath, targetPath, "", sourceExtractPath)
	defer cleanup()

	cfg = &seatbelt.Config{
		ShadowPath: shadow_file.Name(),
	}
	metrics, err = seatbelt.UpdateShadow(context.Background(), cfg, data_files)
	if err != nil {
		t.Fatalf("Failed to update shadow: %v", err)
	}

	assert.Equal(t, int64(3), metrics.SourceSize)
	assert.Equal(t, int64(3), metrics.TargetSize)
	assert.Equal(t, int64(3), metrics.SeatbeltSize)
	assert.Equal(t, int64(1), metrics.ValidCount)
	assert.Equal(t, int64(0), metrics.PendingCount)
	assert.Equal(t, int64(2), metrics.ErrorCount)
}

func TestUpdateShadow_CorruptUpdate(t *testing.T) {
	// sourcePath1 := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "source-scan-corrupt-update-1.csv")
	sourcePath2 := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "source-scan-corrupt-update-2.csv")
	targetPath1 := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "target-scan-corrupt-update-1.csv")
	targetPath2 := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "target-scan-corrupt-update-2.csv")
	sourceExtractPath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "source-extract-corrupt-update.csv")
	changes := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "source-changes-corrupt-update.csv")
	changesBlank := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "source-changes-corrupt-update-blank.csv")

	data_files, cleanup := buildDataFileSet(t, "", targetPath1, sourceExtractPath, "")
	defer cleanup()

	shadow_file, err := os.CreateTemp("", "shadow_initial.dat")
	if err != nil {
		t.Fatalf("Failed to create shadow file: %v", err)
	}
	defer os.Remove(shadow_file.Name())

	cfg := &seatbelt.Config{
		ShadowPath:  shadow_file.Name(),
		InitialLoad: true, // Set initial load flag
	}
	os.Remove(shadow_file.Name())

	// Initial load
	metrics, err := seatbelt.UpdateShadow(context.Background(), cfg, data_files)
	if err != nil {
		t.Fatalf("Failed to update shadow with initial load: %v", err)
	}

	assert.Equal(t, int64(3), metrics.SourceSize)
	assert.Equal(t, int64(3), metrics.TargetSize)
	assert.Equal(t, int64(3), metrics.SeatbeltSize)
	assert.Equal(t, int64(3), metrics.ValidCount)
	assert.Equal(t, int64(0), metrics.PendingCount)
	assert.Equal(t, int64(0), metrics.ErrorCount)

	// Subsequent load
	data_files, cleanup = buildDataFileSet(t, sourcePath2, targetPath2, "", changes)
	defer cleanup()

	cfg = &seatbelt.Config{
		ShadowPath: shadow_file.Name(),
	}
	metrics, err = seatbelt.UpdateShadow(context.Background(), cfg, data_files)
	if err != nil {
		t.Fatalf("Failed to update shadow: %v", err)
	}

	assert.Equal(t, int64(3), metrics.SourceSize)
	assert.Equal(t, int64(3), metrics.TargetSize)
	assert.Equal(t, int64(3), metrics.SeatbeltSize)
	assert.Equal(t, int64(2), metrics.ValidCount)
	assert.Equal(t, int64(1), metrics.PendingCount)
	assert.Equal(t, int64(0), metrics.ErrorCount)

	// Subsequent check
	data_files, cleanup = buildDataFileSet(t, sourcePath2, targetPath2, "", changesBlank)
	defer cleanup()

	cfg = &seatbelt.Config{
		ShadowPath: shadow_file.Name(),
	}
	metrics, err = seatbelt.UpdateShadow(context.Background(), cfg, data_files)
	if err != nil {
		t.Fatalf("Failed to update shadow: %v", err)
	}

	assert.Equal(t, int64(3), metrics.SourceSize)
	assert.Equal(t, int64(3), metrics.TargetSize)
	assert.Equal(t, int64(3), metrics.SeatbeltSize)
	assert.Equal(t, int64(2), metrics.ValidCount)
	assert.Equal(t, int64(0), metrics.PendingCount)
	assert.Equal(t, int64(1), metrics.ErrorCount)
}


// buildDataFileSet is a helper function to create a DataFileSet from file paths.
// It opens the files specified by non-empty paths and populates the DataFileSet.
// It's the caller's responsibility to close the files in the returned DataFileSet.
func buildDataFileSet(t *testing.T, sourceScanPath, targetScanPath, sourceExtractScanPath, sourceChangesPath string) (*seatbelt.DataFileSet, func()) {
	t.Helper()
	filesToClose := []*os.File{}
	cleanup := func() {
		for _, f := range filesToClose {
			f.Close()
		}
	}

	var sourceScanFile, targetScanFile, sourceExtractScanFile, sourceChangesFile *os.File
	var err error

	dataFiles := &seatbelt.DataFileSet{}

	if sourceScanPath != "" {
		sourceScanFile, err = os.OpenFile(sourceScanPath, os.O_RDONLY, 0644)
		if err != nil {
			t.Fatalf("Failed to open source scan file %s: %v", sourceScanPath, err)
		}
		filesToClose = append(filesToClose, sourceScanFile)
		dataFiles.SourceScan = &seatbelt.DataFile{File: sourceScanFile}
	}

	if targetScanPath != "" {
		targetScanFile, err = os.OpenFile(targetScanPath, os.O_RDONLY, 0644)
		if err != nil {
			t.Fatalf("Failed to open target scan file %s: %v", targetScanPath, err)
		}
		filesToClose = append(filesToClose, targetScanFile)
		dataFiles.TargetScan = &seatbelt.DataFile{File: targetScanFile}
	}

	if sourceExtractScanPath != "" {
		sourceExtractScanFile, err = os.OpenFile(sourceExtractScanPath, os.O_RDONLY, 0644)
		if err != nil {
			t.Fatalf("Failed to open source extract scan file %s: %v", sourceExtractScanPath, err)
		}
		filesToClose = append(filesToClose, sourceExtractScanFile)
		dataFiles.SourceExtractScan = &seatbelt.DataFile{File: sourceExtractScanFile}
	}

	if sourceChangesPath != "" {
		sourceChangesFile, err = os.OpenFile(sourceChangesPath, os.O_RDONLY, 0644)
		if err != nil {
			t.Fatalf("Failed to open source changes file %s: %v", sourceChangesPath, err)
		}
		filesToClose = append(filesToClose, sourceChangesFile)
		dataFiles.SourceChanges = &seatbelt.DataFile{File: sourceChangesFile}
	}

	return dataFiles, cleanup
}
