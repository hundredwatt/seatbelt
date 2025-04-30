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
}

type Target interface {
	Scan(ctx context.Context, table Table) (*DataFile, error)
}
