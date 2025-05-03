package seatbelt_test

import (
	"context"
	"testing"

	"seatbelt/pkg/seatbelt"

	"github.com/stretchr/testify/assert"
)

func TestFetchData(t *testing.T) {
	table := &seatbelt.DefaultTable{
		TableDefinition:    *table_definition,
		RowMapperAndHasher: &TestingRowMapperAndHasher{},
	}
	source := &TestingSource{Data: source_data}
	target := &TestingTarget{Data: target_data}

	cfg := &seatbelt.Config{
		Table:  table,
		Source: source,
		Target: target,
	}

	result, err := seatbelt.FetchData(context.Background(), cfg)
	assert.NoError(t, err)

	assert.NotNil(t, result.TargetScan)
	assert.NotNil(t, result.SourceScan)
	assert.NotNil(t, result.SourceChanges)
	assert.Nil(t, result.SourceExtractScan)
}

func TestFetchData_InitialLoad(t *testing.T) {
	table := &seatbelt.DefaultTable{
		TableDefinition:    *table_definition,
		RowMapperAndHasher: &TestingRowMapperAndHasher{},
	}
	source := &TestingSource{Data: source_data}
	target := &TestingTarget{Data: target_data}

	cfg := &seatbelt.Config{
		Table:             table,
		Source:            source,
		Target:            target,
		InitialLoad:       true,
		TestingSourceScan: true,
	}

	result, err := seatbelt.FetchData(context.Background(), cfg)
	assert.NoError(t, err)

	assert.NotNil(t, result.TargetScan)
	assert.NotNil(t, result.SourceScan)
	assert.Nil(t, result.SourceChanges)
	assert.NotNil(t, result.SourceExtractScan)
}
