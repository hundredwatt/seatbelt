package seatbelt

import (
	"fmt"
	"os"
	"time"
)

type DataFile struct {
	File       *os.File
	RowCounter int64
	GenerationTime time.Duration
	SourceDataSize int64
}

func NewDataFile(file *os.File) *DataFile {
	return &DataFile{
		File: file,
	}
}

func (f *DataFile) Name() string {
	return f.File.Name()
}

func (f *DataFile) WriteHeaderLine(format string, a ...interface{}) (int, error) {
	return fmt.Fprintf(f.File, format+"\n", a...)
}

func (f *DataFile) WriteLine(format string, a ...interface{}) (int, error) {
	f.RowCounter++
	return fmt.Fprintf(f.File, format+"\n", a...)
}

func (f *DataFile) Close() error {
	return f.File.Close()
}

func (f *DataFile) IncrementRowCounter() {
	f.RowCounter++
}

func (f *DataFile) SetRowCounter(rowCounter int64) {
	f.RowCounter = rowCounter
}

func (f *DataFile) RowCount() int64 {
	return f.RowCounter
}

func (f *DataFile) Rewind() error {
	_, err := f.File.Seek(0, 0)
	return err
}

func (f *DataFile) SetGenerationTime(generationTime time.Duration) {
	f.GenerationTime = generationTime
}

func (f *DataFile) SetSourceDataSize(sourceDataSize int64) {
	f.SourceDataSize = sourceDataSize
}

func (f *DataFile) FileSize() (int64, error) {
	fileInfo, err := f.File.Stat()
	if err != nil {
		return 0, err
	}
	return fileInfo.Size(), nil
}