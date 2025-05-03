package seatbelt

import (
	"fmt"
	"os"
)

type DataFile struct {
	File       *os.File
	RowCounter int64
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
