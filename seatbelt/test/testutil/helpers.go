package testutil

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

func FindLineWithPrefix(file *os.File, prefix string) (string, error) {
	file.Seek(0, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), prefix) {
			return scanner.Text(), nil
		}
	}
	return "", fmt.Errorf("line with prefix %s not found", prefix)
}

func Preview(file *os.File, max int) {
	if max >= 0 {
		// Forward reading from the beginning
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
			max--
			if max <= 0 {
				fmt.Println("...")
				break
			}
		}
	} else {
		// Original position
		currentPos, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			fmt.Println("Error seeking file:", err)
			return
		}

		// Reset to beginning to read all lines
		file.Seek(0, 0)
		var full = false
		var tail = 0
		var size = -1 * max
		var ring = make([]string, size)
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fmt.Println("Writing to ring", tail)
			ring[tail] = scanner.Text()
			tail++
			if tail >= size {
				tail = 0
				full = true
			}
		}

		// Print
		if !full {
			for i := 0; i < tail; i++ {
				fmt.Println(ring[i])
			}
		} else {
			for i := 0; i < size; i++ {
				read := (tail + i) % size
				fmt.Println(ring[read])
			}
		}

		// Restore original position
		file.Seek(currentPos, 0)
	}
}
