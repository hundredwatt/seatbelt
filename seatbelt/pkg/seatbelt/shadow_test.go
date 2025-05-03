package seatbelt_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"seatbelt/pkg/seatbelt"

	"github.com/stretchr/testify/assert"
)

func TestUpdateShadow(t *testing.T) {
	sourcePath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "seatbelt-scan-public.data_proof_test-778689522.csv")
	targetPath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "seatbelt-clickhouse-scan-peerdb.public_data_proof-1115524834.csv")
	sourceExtractPath := filepath.Join("..", "..", "test", "testdata", "example_datafiles", "seatbelt-extract-scan-public.data_proof_test-3447664090.csv")

	source_scan_file, err := os.OpenFile(sourcePath, os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open source scan file: %v", err)
	}
	defer source_scan_file.Close()

	target_scan_file, err := os.OpenFile(targetPath, os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open target scan file: %v", err)
	}
	defer target_scan_file.Close()

	source_extract_scan_file, err := os.OpenFile(sourceExtractPath, os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open source extract scan file: %v", err)
	}
	defer source_extract_scan_file.Close()

	data_files := &seatbelt.DataFileSet{
		SourceScan:        &seatbelt.DataFile{File: source_scan_file},
		TargetScan:        &seatbelt.DataFile{File: target_scan_file},
		// Pretend the extract scan is the source changes
		SourceChanges: &seatbelt.DataFile{File: source_extract_scan_file},
	}

	shadow_file, err := os.CreateTemp("", "shadow.csv")
	if err != nil {
		t.Fatalf("Failed to create shadow file: %v", err)
	}
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
