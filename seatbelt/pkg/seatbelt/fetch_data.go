package seatbelt

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sync/errgroup"
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
	defer cancel()

	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		<-signals
		cancel()
	}()

	if cfg.InitialLoad {
		return initialLoad(ctx, cfg)
	}

	return defaultFetchData(ctx, cfg)
}

func defaultFetchData(ctx context.Context, cfg *Config) (*DataFileSet, error) {
	sg, ctx := errgroup.WithContext(ctx)

	var consumer ChangeStreamConsumer
	var target_scan *DataFile
	var source_scan *DataFile

	consumer, err := cfg.Source.StartChangeStreamConsumer(ctx, cfg.Table)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := consumer.Close(); closeErr != nil {
			log.Printf("Error closing change stream consumer: %v", closeErr)
		}
	}()

	sg.Go(func() error {
		var err error
		targetScanStart := time.Now()
		target_scan, err = cfg.Target.Scan(ctx, cfg.Table)
		log.Printf("Target scan completed in %v", time.Since(targetScanStart))
		return err
	})

	sg.Go(func() error {
		var err error
		sourceScanStart := time.Now()
		source_scan, err = cfg.Source.Scan(ctx, cfg.Table)
		log.Printf("Source scan completed in %v", time.Since(sourceScanStart))
		return err
	})

	if err := sg.Wait(); err != nil {
		return nil, err
	}

	consumeStart := time.Now()
	source_changes, err := consumer.ConsumeToCompletion()
	log.Printf("Consumer ConsumeToCompletion completed in %v", time.Since(consumeStart))
	if err != nil {
		return nil, err
	}

	return &DataFileSet{
		TargetScan:    target_scan,
		SourceScan:    source_scan,
		SourceChanges: source_changes,
	}, nil
}

func initialLoad(ctx context.Context, cfg *Config) (*DataFileSet, error) {
	sg, ctx := errgroup.WithContext(ctx)

	// TODO - advance consumer log position to reasonable value

	var target_scan *DataFile
	var source_scan *DataFile
	var source_extract_scan *DataFile

	sg.Go(func() error {
		targetScanStart := time.Now()
		var err error
		target_scan, err = cfg.Target.Scan(ctx, cfg.Table)
		log.Printf("Target scan completed in %v", time.Since(targetScanStart))
		return err
	})

	sg.Go(func() error {
		sourceExtractScanStart := time.Now()
		var err error
		source_extract_scan, err = cfg.Source.ExtractScan(ctx, cfg.Table)
		log.Printf("Source extract scan completed in %v", time.Since(sourceExtractScanStart))
		return err
	})

	if cfg.TestingSourceScan {
		sg.Go(func() error {
			sourceScanStart := time.Now()
			var err error
			source_scan, err = cfg.Source.Scan(ctx, cfg.Table)
			log.Printf("Source scan completed in %v", time.Since(sourceScanStart))
			return err
		})
	}

	if err := sg.Wait(); err != nil {
		return nil, err
	}

	return &DataFileSet{
		TargetScan:        target_scan,
		SourceScan:        source_scan,
		SourceExtractScan: source_extract_scan,
	}, nil
}
