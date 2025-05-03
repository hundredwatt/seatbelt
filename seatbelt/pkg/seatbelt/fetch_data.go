package seatbelt

import (
	"context"
	"os"
	"os/signal"
	"sync"
)

type Config struct {
	Table             Table
	Source            Source
	Target            Target
	InitialLoad       bool
	TestingSourceScan bool
	ShadowPath        string
}

type DataFileSet struct {
	TargetScan        *DataFile
	SourceScan        *DataFile
	SourceChanges     *DataFile
	SourceExtractScan *DataFile
}

func FetchData(ctx context.Context, cfg *Config) (*DataFileSet, error) {
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		<-signals
		cancel()
	}()

	if cfg.InitialLoad {
		return initialLoad(ctx, cfg, cancel)
	}

	return defaultFetchData(ctx, cfg, cancel)
}

func defaultFetchData(ctx context.Context, cfg *Config, cancel context.CancelFunc) (*DataFileSet, error) {
	sg := sync.WaitGroup{}

	var consumer ChangeStreamConsumer
	var target_scan *DataFile
	var target_scan_err error
	var source_scan *DataFile
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

	sg.Add(1)
	go func() {
		defer sg.Done()
		source_scan, source_scan_err = cfg.Source.Scan(ctx, cfg.Table)
	}()

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

	if target_scan_err != nil {
		return nil, target_scan_err
	}

	if source_scan_err != nil {
		return nil, source_scan_err
	}

	source_changes, err := consumer.ConsumeToCompletion()
	if err != nil {
		return nil, err
	}

	return &DataFileSet{
		TargetScan:    target_scan,
		SourceScan:    source_scan,
		SourceChanges: source_changes,
	}, nil
}

func initialLoad(ctx context.Context, cfg *Config, cancel context.CancelFunc) (*DataFileSet, error) {
	sg := sync.WaitGroup{}

	// TODO - advance consumer log position to reasonable value

	var target_scan *DataFile
	var target_scan_err error
	var source_scan *DataFile
	var source_scan_err error
	var source_extract_scan *DataFile
	var source_extract_scan_err error

	sg.Add(1)
	go func() {
		defer sg.Done()
		target_scan, target_scan_err = cfg.Target.Scan(ctx, cfg.Table)
	}()

	sg.Add(1)
	go func() {
		defer sg.Done()
		source_extract_scan, source_extract_scan_err = cfg.Source.ExtractScan(ctx, cfg.Table)
	}()

	if cfg.TestingSourceScan {
		sg.Add(1)
		go func() {
			defer sg.Done()
			source_scan, source_scan_err = cfg.Source.Scan(ctx, cfg.Table)
		}()
	}

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

	if target_scan_err != nil {
		return nil, target_scan_err
	}

	if source_scan_err != nil {
		return nil, source_scan_err
	}

	if source_extract_scan_err != nil {
		return nil, source_extract_scan_err
	}

	return &DataFileSet{
		TargetScan:        target_scan,
		SourceScan:        source_scan,
		SourceExtractScan: source_extract_scan,
	}, nil
}
