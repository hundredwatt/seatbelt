package exp

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"sync"
)

/* Schema */
type ColumnType string

const (
	ColumnTypeInt   ColumnType = "int"
	ColumnTypeFloat ColumnType = "float"
	ColumnTypeText  ColumnType = "text"
	ColumnTypeNull  ColumnType = ""
)

type Column struct {
	Name string
	Type ColumnType
}

type ColumnMapping struct {
	Name       string
	SourceType ColumnType
	TargetType ColumnType
}

type TableDefinition struct {
	TableName string
	Columns   []ColumnMapping
}

func (t *TableDefinition) Name() string {
	return t.TableName
}

func (t *TableDefinition) ColumnMapping() []ColumnMapping {
	return t.Columns
}

func (t *TableDefinition) SourceColumns() []Column {
	columns := make([]Column, len(t.Columns))
	for i, column := range t.Columns {
		if column.SourceType == ColumnTypeNull {
			continue
		}
		columns[i] = Column{Name: column.Name, Type: column.SourceType}
	}
	return columns
}

func (t *TableDefinition) TargetColumns() []Column {
	columns := make([]Column, len(t.Columns))
	for i, column := range t.Columns {
		if column.TargetType == ColumnTypeNull {
			continue
		}
		columns[i] = Column{Name: column.Name, Type: column.TargetType}
	}
	return columns
}

/* RowHash Types */
type RowHash interface {
	// Empty interface to allow either uint64 or [16]byte
	// Implementations should return either uint64 or [16]byte
}

type Uint64Hash uint64

func (h Uint64Hash) String() string {
	return fmt.Sprintf("%d", h)
}

type Hex32Hash [32]byte

func (h Hex32Hash) String() string {
	return hex.EncodeToString(h[:])
}

type RowMapperAndHasher interface {
	FormatSource(row []interface{}) (string, error)
	TransformSourceToCommon(row []interface{}) (string, error)
	TransformTargetToCommon(row []interface{}) (string, error)

	SourceHash(data string) RowHash
	TargetHash(data string) RowHash
}

/* Table Interface */
type Table interface {
	Name() string
	ColumnMapping() []ColumnMapping
	SourceColumns() []Column
	TargetColumns() []Column

	RowMapperAndHasher
}

/* Seatbelt System */
type Source interface {
	Scan(ctx context.Context, table Table) (*os.File, error)
	ExtractScan(ctx context.Context, table Table) (*os.File, error)

	// TODO - needs to support multiple tables
	StartChangeStreamConsumer(ctx context.Context, table Table) (ChangeStreamConsumer, error)
}

type ChangeStreamConsumer interface {
	ConsumeToCompletion() (*os.File, error)
}

type Target interface {
	Scan(ctx context.Context, table Table) (*os.File, error)
}

/* Perform */
type Config struct {
	Table  Table
	Source Source
	Target Target
}

type IntermediateResult struct {
	TargetScanRows    int
	SourceScanRows    int
	SourceChangesRows int
}

func Perform(ctx context.Context, cfg *Config) (*IntermediateResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	sg := sync.WaitGroup{}

	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		<-signals
		cancel()
	}()

	var target_scan *os.File
	var target_scan_err error
	var source_scan *os.File
	var source_scan_err error

	consumer, err := cfg.Source.StartChangeStreamConsumer(ctx, cfg.Table)
	if err != nil {
		cancel()
		return nil, err
	}

	sg.Add(1)
	go func() {
		defer sg.Done()
		target_scan, target_scan_err = cfg.Target.Scan(ctx, cfg.Table)
	}()
	defer target_scan.Close()

	sg.Add(1)
	go func() {
		defer sg.Done()
		source_scan, source_scan_err = cfg.Source.Scan(ctx, cfg.Table)
	}()
	defer source_scan.Close()

	done := make(chan struct{})
	go func() {
		sg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// WaitGroup completed normally
	case <-ctx.Done():
		// Context was cancelled
		return nil, ctx.Err()
	}

	if err := any(target_scan_err, source_scan_err); err != nil {
		return nil, err
	}

	source_changes, err := consumer.ConsumeToCompletion()
	if err != nil {
		return nil, err
	}
	defer source_changes.Close()

	// wc -l target_scan
	target_scan_rows, err := wc_l(target_scan)
	must(err)

	source_scan_rows, err := wc_l(source_scan)
	must(err)

	source_changes_rows, err := wc_l(source_changes)
	must(err)

	return &IntermediateResult{
		TargetScanRows:    target_scan_rows,
		SourceScanRows:    source_scan_rows,
		SourceChangesRows: source_changes_rows,
	}, nil
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func any(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func wc_l(file *os.File) (int, error) {
	file.Seek(0, 0)
	scanner := bufio.NewScanner(file)
	rows := 0
	for scanner.Scan() {
		rows++
	}
	return rows, nil
}
