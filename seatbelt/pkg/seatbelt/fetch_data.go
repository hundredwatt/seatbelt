package seatbelt

import (
	"context"
	"log/slog"
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
	sg, errgroupCtx := errgroup.WithContext(ctx)

	var consumer ChangeStreamConsumer
	var target_scan *DataFile
	var source_scan *DataFile

	consumer, err := cfg.Source.StartChangeStreamConsumer(ctx, cfg.Table)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := consumer.Close(); closeErr != nil {
			slog.Error("Error closing change stream consumer", "error", closeErr)
		}
	}()

	sg.Go(func() error {
		var err error
		targetDataSize, err := cfg.Target.DataSize(errgroupCtx, cfg.Table)
		if err != nil {
			return err
		}
		targetScanStart := time.Now()
		target_scan, err = cfg.Target.Scan(errgroupCtx, cfg.Table)
		if err != nil {
			return err
		}
		target_scan.SetGenerationTime(time.Since(targetScanStart))
		target_scan.SetSourceDataSize(targetDataSize)
		slog.Debug("Target scan completed", "duration", target_scan.GenerationTime)
		return nil
	})

	sg.Go(func() error {
		var err error
		sourceDataSize, err := cfg.Source.DataSize(errgroupCtx, cfg.Table)
		if err != nil {
			return err
		}
		sourceScanStart := time.Now()
		source_scan, err = cfg.Source.Scan(errgroupCtx, cfg.Table)
		if err != nil {
			return err
		}
		source_scan.SetGenerationTime(time.Since(sourceScanStart))
		source_scan.SetSourceDataSize(sourceDataSize)
		slog.Debug("Source scan completed", "duration", source_scan.GenerationTime)
		return nil
	})

	if err := sg.Wait(); err != nil {
		return nil, err
	}

	consumeStart := time.Now()
	source_changes, err := consumer.ConsumeToCompletion()
	source_changes.SetGenerationTime(time.Since(consumeStart))
	slog.Debug("Consumer ConsumeToCompletion completed", "duration", source_changes.GenerationTime)
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
		targetDataSize, err := cfg.Target.DataSize(ctx, cfg.Table)
		if err != nil {
			return err
		}
		targetScanStart := time.Now()
		target_scan, err = cfg.Target.Scan(ctx, cfg.Table)
		if err != nil {
			return err
		}
		target_scan.SetGenerationTime(time.Since(targetScanStart))
		target_scan.SetSourceDataSize(targetDataSize)
		slog.Debug("Target scan completed", "duration", target_scan.GenerationTime)
		return nil
	})

	sg.Go(func() error {
		sourceDataSize, err := cfg.Source.DataSize(ctx, cfg.Table)
		if err != nil {
			return err
		}
		sourceExtractScanStart := time.Now()
		source_extract_scan, err = cfg.Source.ExtractScan(ctx, cfg.Table)
		if err != nil {
			return err
		}
		source_extract_scan.SetGenerationTime(time.Since(sourceExtractScanStart))
		source_extract_scan.SetSourceDataSize(sourceDataSize)
		slog.Debug("Source extract scan completed", "duration", source_extract_scan.GenerationTime)
		return nil
	})

	if cfg.TestingSourceScan {
		sg.Go(func() error {
			sourceDataSize, err := cfg.Source.DataSize(ctx, cfg.Table)
			if err != nil {
				return err
			}
			sourceScanStart := time.Now()
			source_scan, err = cfg.Source.Scan(ctx, cfg.Table)
			if err != nil {
				return err
			}
			source_scan.SetGenerationTime(time.Since(sourceScanStart))
			source_scan.SetSourceDataSize(sourceDataSize)
			slog.Debug("Source scan completed", "duration", source_scan.GenerationTime)
			return nil
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
