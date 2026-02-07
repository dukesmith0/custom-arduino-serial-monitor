package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"
)

// CSVExportOptions configures how serial data is exported to CSV.
type CSVExportOptions struct {
	FilePath          string
	IncludeTimestamps bool
	FilterByTime      bool
	StartTime         time.Time
	EndTime           time.Time
	CustomHeader      []string // Custom header row; if nil, default or no header is used.
}

// ExportCSV writes the given serial lines to a CSV file based on the provided options.
func ExportCSV(lines []SerialLine, opts CSVExportOptions) error {
	f, err := os.Create(opts.FilePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)

	// Write header
	if len(opts.CustomHeader) > 0 {
		if err := w.Write(opts.CustomHeader); err != nil {
			return fmt.Errorf("failed to write custom header: %w", err)
		}
	} else if opts.IncludeTimestamps {
		if err := w.Write([]string{"Timestamp", "Data"}); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}
	} else {
		if err := w.Write([]string{"Data"}); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}
	}

	for _, line := range lines {
		if opts.FilterByTime {
			if line.Timestamp.Before(opts.StartTime) || line.Timestamp.After(opts.EndTime) {
				continue
			}
		}

		fields := strings.Split(line.Data, ",")
		var record []string
		if opts.IncludeTimestamps {
			record = append([]string{line.Timestamp.Format("2006-01-02 15:04:05.000")}, fields...)
		} else {
			record = fields
		}

		if err := w.Write(record); err != nil {
			return fmt.Errorf("failed to write record: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("failed to flush csv writer: %w", err)
	}
	return nil
}

// ParseCustomHeader reads a single-line CSV file and returns the fields as a header row.
func ParseCustomHeader(filePath string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open header file: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	record, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	return record, nil
}
