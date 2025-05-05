package seatbelt_test

import (
	"bufio"
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"seatbelt/pkg/seatbelt"

	"github.com/stretchr/testify/assert"
	"github.com/zeebo/xxh3"
)

/* Utilities */
var testDir string

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func wc_l(file *os.File) (int, error) {
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count, nil
}

func ensureTmpDir() (string, error) {
	// Get the current working directory
	cwd, err := os.Getwd()
	must(err)
	tmpDir := filepath.Join(cwd, "..", "tmp")
	_, err = os.Stat(tmpDir)
	if os.IsNotExist(err) {
		must(os.Mkdir(tmpDir, 0755))
	} else if err != nil {
		must(err)
	}

	return tmpDir, nil
}

func ensureTimestampedTmpDir(dir string) (string, error) {
	timestamp := time.Now().Format("20060102150405")
	timestampedDir := filepath.Join(dir, timestamp)
	_, err := os.Stat(timestampedDir)
	if os.IsNotExist(err) {
		must(os.Mkdir(timestampedDir, 0755))
	} else if err != nil {
		must(err)
	}
	return timestampedDir, nil
}

func init() {
	var err error
	tmpDir, err := ensureTmpDir()
	must(err)
	testDir, err = ensureTimestampedTmpDir(tmpDir)
	must(err)

	fmt.Println("Test directory:", testDir)
}

/* Test Data Sources */
var table_definition = &seatbelt.TableDefinition{
	SourceDatabase: seatbelt.POSTGRES,
	TargetDatabase: seatbelt.POSTGRES,
	TableName:      "test",
	Columns: []seatbelt.ColumnMapping{
		{Name: "id", SourceType: "integer", TargetType: "integer"},
		{Name: "name", SourceType: "text", TargetType: "text"},
		{Name: "score", SourceType: "integer", TargetType: "real"},
	},
}

var source_data = []map[string]interface{}{
	{
		"id":    1,
		"name":  "John",
		"score": 100,
	},
	{
		"id":    2,
		"name":  "Jane",
		"score": 200,
	},
	{
		"id":    3,
		"name":  "Jim",
		"score": 300,
	},
}

var source_changes = source_data[1:]

type TestingSource struct {
	Data []map[string]interface{}
}

func (s *TestingSource) Scan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	osfile, err := os.Create(filepath.Join(testDir, "source_scan.txt"))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)
	for _, row := range s.Data {
		values := make([]interface{}, len(row))
		for i, column := range table.SourceColumns() {
			values[i] = row[column.Name]
		}
		row_string, err := table.FormatSource(values)
		if err != nil {
			return nil, err
		}
		source_hash := table.SourceHash(row_string)
		file.WriteLine("%d,%d,%s", row["id"], source_hash, row_string)
	}

	file.Rewind()
	return file, nil
}

func (s *TestingSource) ExtractScan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	osfile, err := os.Create(filepath.Join(testDir, "source_extract_scan.txt"))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)

	for _, row := range s.Data {
		source_values := make([]interface{}, len(row))
		for i, column := range table.SourceColumns() {
			source_values[i] = row[column.Name]
		}
		row_string, err := table.FormatSource(source_values)
		source_hash := table.SourceHash(row_string)

		target_row_string, err := table.TransformSourceToCommon(source_values)
		if err != nil {
			return nil, err
		}
		target_hash := table.TargetHash(target_row_string)
		file.WriteLine("%d,%d,%s,%s,%s", row["id"], source_hash, target_hash, row_string, target_row_string)
	}

	file.Rewind()
	return file, nil
}

func (s *TestingSource) StartChangeStreamConsumer(ctx context.Context, table seatbelt.Table) (seatbelt.ChangeStreamConsumer, error) {
	return NewTestingChangeStreamConsumer(table)
}

type TestingChangeStreamConsumer struct {
	Data         []map[string]interface{}
	OutputFile   *seatbelt.DataFile
	Complete     chan struct{}
	ErrorChannel chan error
	WaitGroup    *sync.WaitGroup
	Context      context.Context
	Table        seatbelt.Table
}

func NewTestingChangeStreamConsumer(table seatbelt.Table) (*TestingChangeStreamConsumer, error) {
	osfile, err := os.Create(filepath.Join(testDir, "source_changes.txt"))
	if err != nil {
		return nil, err
	}
	output_file := seatbelt.NewDataFile(osfile)
	complete := make(chan struct{})
	error_channel := make(chan error)
	consumer := &TestingChangeStreamConsumer{Data: source_changes, OutputFile: output_file, Complete: complete, ErrorChannel: error_channel, WaitGroup: &sync.WaitGroup{}, Context: context.Background(), Table: table}
	err = consumer.startReader()
	if err != nil {
		return nil, err
	}
	return consumer, nil
}

func (c *TestingChangeStreamConsumer) startReader() error {
	go func() {
		index := 0
		completionRequested := false

		process_record := func(index int) {
			row := c.Data[index]
			source_values := make([]interface{}, len(row))
			for i, column := range c.Table.SourceColumns() {
				source_values[i] = row[column.Name]
			}
			row_string, err := c.Table.FormatSource(source_values)
			if err != nil {
				c.ErrorChannel <- err
				return
			}
			source_hash := c.Table.SourceHash(row_string)

			target_row_string, err := c.Table.TransformSourceToCommon(source_values)
			if err != nil {
				c.ErrorChannel <- err
				return
			}
			target_hash := c.Table.TargetHash(target_row_string)
			_, err = c.OutputFile.WriteLine("%d,%d,%s,%s,%s", row["id"], source_hash, target_hash, row_string, target_row_string)
			if err != nil {
				c.ErrorChannel <- err
				return
			}
		}

		c.WaitGroup.Add(1)
		tick := make(chan struct{}, 1)
		tick <- struct{}{}

		for {
			select {
			case <-tick:
				// Check if we have more records to process
				if index < len(c.Data) {
					c.WaitGroup.Add(1)
					process_record(index)
					index++
					c.WaitGroup.Done()
					tick <- struct{}{}
				} else if completionRequested {
					// All records processed and completion was requested, so we can exit
					c.WaitGroup.Done()
					return
				} else {
					time.Sleep(50 * time.Millisecond)
					tick <- struct{}{}
				}
			case <-c.Complete:
				completionRequested = true
			case <-c.Context.Done():
				c.WaitGroup.Done()
				return
			}
		}
	}()
	return nil
}

func (c *TestingChangeStreamConsumer) ConsumeToCompletion() (*seatbelt.DataFile, error) {
	c.Complete <- struct{}{}

	done := make(chan struct{})
	go func() {
		c.WaitGroup.Wait()
		close(done)
	}()

	select {
	case err := <-c.ErrorChannel:
		return nil, err
	case <-done:
		// WaitGroup completed successfully
	}

	return c.OutputFile, nil
}

type TestingTarget struct {
	Data []map[string]interface{}
}

var target_data = []map[string]interface{}{
	{
		"id":    1,
		"name":  "John",
		"score": 100.0,
	},
	{
		"id":    2,
		"name":  "Jane",
		"score": 200.0,
	},
	{
		"id":    3,
		"name":  "Jim",
		"score": 300.0,
	},
}

func (t *TestingTarget) Scan(ctx context.Context, table seatbelt.Table) (*seatbelt.DataFile, error) {
	osfile, err := os.Create(filepath.Join(testDir, "target_scan.txt"))
	if err != nil {
		return nil, err
	}
	file := seatbelt.NewDataFile(osfile)
	for _, row := range t.Data {
		values := make([]interface{}, len(row))
		for i, column := range table.TargetColumns() {
			values[i] = row[column.Name]
		}
		row_string, err := table.TransformTargetToCommon(values)
		if err != nil {
			return nil, err
		}
		target_hash := table.TargetHash(row_string)

		file.WriteLine("%d,%s,%s", row["id"], target_hash, row_string)
	}

	file.Rewind()
	return file, nil
}

type TestingRowMapperAndHasher struct {
}

func (h *TestingRowMapperAndHasher) FormatSource(row []interface{}) (string, error) {
	return fmt.Sprintf("%s|%d", row[1], row[2]), nil
}

func (h *TestingRowMapperAndHasher) TransformSourceToCommon(row []interface{}) (string, error) {
	// Convert score to float64 to ensure proper formatting
	score, ok := row[2].(float64)
	if !ok {
		// Handle the case where score is an int
		if scoreInt, ok := row[2].(int); ok {
			score = float64(scoreInt)
		}
	}
	return fmt.Sprintf("%s|%.1f", row[1], score), nil
}

func (h *TestingRowMapperAndHasher) TransformTargetToCommon(row []interface{}) (string, error) {
	return fmt.Sprintf("%s|%.1f", row[1], row[2]), nil
}

func (h *TestingRowMapperAndHasher) SourceHash(data string) seatbelt.RowHash {
	return seatbelt.Uint64Hash(xxh3.Hash([]byte(data)))
}

func (h *TestingRowMapperAndHasher) TargetHash(data string) seatbelt.RowHash {
	hasher := md5.New()
	hasher.Write([]byte(data))

	return seatbelt.Hex16Hash(hasher.Sum(nil)[0:16])
}

var table = &seatbelt.DefaultTable{
	TableDefinition:    *table_definition,
	RowMapperAndHasher: &TestingRowMapperAndHasher{},
}

/* Tests */
func TestRowMapperAndHasher(t *testing.T) {
	test_row := map[string]interface{}{
		"id":    1,
		"name":  "John",
		"score": 100,
	}

	source_values := make([]interface{}, len(test_row))
	for i, column := range table_definition.SourceColumns() {
		source_values[i] = test_row[column.Name]
	}
	target_values := make([]interface{}, len(test_row))
	for i, column := range table_definition.TargetColumns() {
		if column.Name == "score" {
			target_values[i] = float64(test_row[column.Name].(int))
		} else {
			target_values[i] = test_row[column.Name]
		}
	}

	row_mapper_and_hasher := &TestingRowMapperAndHasher{}

	test_row_string, err := row_mapper_and_hasher.FormatSource(source_values)
	must(err)

	common_from_source_string, err := row_mapper_and_hasher.TransformSourceToCommon(source_values)
	must(err)

	common_from_target_string, err := row_mapper_and_hasher.TransformTargetToCommon(target_values)
	must(err)

	source_hash := row_mapper_and_hasher.SourceHash(test_row_string)
	target_hash_from_source := row_mapper_and_hasher.TargetHash(common_from_source_string)
	target_hash_from_target := row_mapper_and_hasher.TargetHash(common_from_target_string)

	assert.Equal(t, test_row_string, "John|100")
	assert.Equal(t, common_from_source_string, "John|100.0")
	assert.Equal(t, common_from_target_string, "John|100.0")
	assert.Equal(t, source_hash, seatbelt.Uint64Hash(6710712646738599732))
	assert.Equal(t, target_hash_from_source.String(), "c42581752697e38e6bf312112469d28c")
	assert.Equal(t, target_hash_from_target.String(), "c42581752697e38e6bf312112469d28c")
}

func TestExtractScan(t *testing.T) {
	table := &seatbelt.DefaultTable{
		TableDefinition:    *table_definition,
		RowMapperAndHasher: &TestingRowMapperAndHasher{},
	}
	source := &TestingSource{Data: source_data}

	extract_scan, err := source.ExtractScan(context.Background(), table)
	must(err)

	rows, err := wc_l(extract_scan.File)
	must(err)

	assert.Equal(t, rows, 3)
}
