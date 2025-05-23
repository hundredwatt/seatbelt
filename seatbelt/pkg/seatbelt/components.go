package seatbelt

import (
	"context"
)

type Source interface {
	Scan(ctx context.Context, table Table) (*DataFile, error)
	ExtractScan(ctx context.Context, table Table) (*DataFile, error)

	// TODO - needs to support multiple tables
	StartChangeStreamConsumer(ctx context.Context, table Table) (ChangeStreamConsumer, error)
}

type ChangeStreamConsumer interface {
	ConsumeToCompletion() (*DataFile, error)
	Close() error
}

type Target interface {
	Scan(ctx context.Context, table Table) (*DataFile, error)
}

// Inspector interface
type SourceInspector interface {
	InspectScan(ctx context.Context, table Table, primaryKeys []int64) (*DataFile, error)
	InspectExtractScan(ctx context.Context, table Table, primaryKeys []int64) (*DataFile, error)
}

type TargetInspector interface {
	InspectScan(ctx context.Context, table Table, primaryKeys []int64) (*DataFile, error)
}
