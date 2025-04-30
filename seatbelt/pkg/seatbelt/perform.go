package seatbelt

import (
	"context"
	"os"
	"os/signal"
	"sync"
)

type PerformIncompleteResult struct {
	TargetScan *DataFile
	SourceScan *DataFile
	SourceChanges *DataFile
}

func Perform(ctx context.Context, cfg *Config) (*PerformIncompleteResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	sg := sync.WaitGroup{}

	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		<-signals
		cancel()
	}()

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
	// defer func() {
	// 	if target_scan != nil {
	// 		target_scan.Close()
	// 	}
	// }()

	sg.Add(1)
	go func() {
		defer sg.Done()
		source_scan, source_scan_err = cfg.Source.Scan(ctx, cfg.Table)
	}()
	// defer func() {
	// 	if source_scan != nil {
	// 		source_scan.Close()
	// 	}
	// }()

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
	// defer source_changes.Close()

	return &PerformIncompleteResult{
		TargetScan: target_scan,
		SourceScan: source_scan,
		SourceChanges: source_changes,
	}, nil
}
