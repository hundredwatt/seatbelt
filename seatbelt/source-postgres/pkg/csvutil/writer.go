package csvutil

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
)

// WriteIDHashMapToCSV writes a map[int32]int64 to a CSV file.
func WriteIDHashMapToCSV(filePath string, data map[int32]int64, idColumnName, hashColumnName string) error {
	log.Printf("Writing data to CSV: %s", filePath)
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create CSV file '%s': %w", filePath, err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{idColumnName, hashColumnName}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header to '%s': %w", filePath, err)
	}

	// Write data rows
	count := 0
	for id, hash := range data {
		row := []string{
			strconv.FormatInt(int64(id), 10),
			strconv.FormatInt(hash, 10),
		}
		if err := writer.Write(row); err != nil {
			// Log error but attempt to continue writing other rows
			log.Printf("Error writing row (ID: %d) to CSV '%s': %v", id, filePath, err)
		}
		count++
	}

	if err := writer.Error(); err != nil {
		return fmt.Errorf("error occurred during CSV writing to '%s': %w", filePath, err)
	}

	log.Printf("Successfully wrote %d rows to CSV: %s", count, filePath)
	return nil
}
