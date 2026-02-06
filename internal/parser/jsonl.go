package parser

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/bardisty/ccusage/internal/models"
)

// maxSkippedLineNumbers is the maximum number of skipped line numbers to track.
// This prevents unbounded memory growth on extremely malformed files.
const maxSkippedLineNumbers = 100

// ParseResult contains the parsed messages and any parse warnings
type ParseResult struct {
	Messages     []models.JSONLMessage
	SkippedLines int   // Number of lines that failed to parse
	SkippedAt    []int // Line numbers of skipped lines (1-indexed, capped at maxSkippedLineNumbers)
}

// Warning returns a warning message if any lines were skipped, empty string otherwise
func (r ParseResult) Warning() string {
	if r.SkippedLines == 0 {
		return ""
	}
	if r.SkippedLines == 1 {
		if len(r.SkippedAt) > 0 {
			return fmt.Sprintf("warning: 1 malformed line skipped (line %d)", r.SkippedAt[0])
		}
		return "warning: 1 malformed line skipped"
	}
	// Show first few line numbers if there are many
	if len(r.SkippedAt) > 5 {
		return fmt.Sprintf("warning: %d malformed lines skipped (lines %v and %d more)",
			r.SkippedLines, r.SkippedAt[:5], r.SkippedLines-5)
	}
	return fmt.Sprintf("warning: %d malformed lines skipped (lines %v)", r.SkippedLines, r.SkippedAt)
}

// ParseJSONLFile parses a session JSONL file and returns assistant messages with usage data
func ParseJSONLFile(path string) ([]models.JSONLMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	result, err := ParseJSONLWithResult(file)
	if err != nil {
		return nil, err
	}
	return result.Messages, nil
}

// ParseJSONLFileWithResult parses a session JSONL file and returns detailed results
func ParseJSONLFileWithResult(path string) (*ParseResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return ParseJSONLWithResult(file)
}

// ParseJSONL parses JSONL content from a reader and returns assistant messages with usage data
func ParseJSONL(r io.Reader) ([]models.JSONLMessage, error) {
	result, err := ParseJSONLWithResult(r)
	if err != nil {
		return nil, err
	}
	return result.Messages, nil
}

// ParseJSONLWithResult parses JSONL content and returns detailed results including skipped lines
func ParseJSONLWithResult(r io.Reader) (*ParseResult, error) {
	result := &ParseResult{}
	scanner := bufio.NewScanner(r)

	// Increase buffer size for large lines (constants defined in sessions.go)
	buf := make([]byte, 0, scannerInitialBufSize)
	scanner.Buffer(buf, scannerMaxBufSize)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg models.JSONLMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// Track skipped lines instead of silently ignoring
			result.SkippedLines++
			if len(result.SkippedAt) < maxSkippedLineNumbers {
				result.SkippedAt = append(result.SkippedAt, lineNum)
			}
			continue
		}

		// Only collect assistant messages with usage data
		if msg.Type == "assistant" && msg.Message != nil {
			result.Messages = append(result.Messages, msg)
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			// Return partial results — one oversized line shouldn't destroy the session
			result.SkippedLines++
			if len(result.SkippedAt) < maxSkippedLineNumbers {
				result.SkippedAt = append(result.SkippedAt, lineNum+1)
			}
			return result, nil
		}
		return nil, err
	}

	return result, nil
}

// ExtractUsageFromMessages extracts token usage data from parsed messages
func ExtractUsageFromMessages(messages []models.JSONLMessage) []models.MessageAnalysis {
	var analyses []models.MessageAnalysis

	for _, msg := range messages {
		if msg.Message == nil {
			continue
		}

		analysis := models.MessageAnalysis{
			Timestamp: msg.Timestamp,
			Model:     msg.Message.Model,
			Usage:     msg.Message.Usage,
		}
		// Reconcile CacheCreationInputTokens with detailed CacheCreation
		if analysis.Usage.CacheCreation != nil {
			analysis.Usage.CacheCreationInputTokens = analysis.Usage.CacheCreation.Ephemeral5mInputTokens + analysis.Usage.CacheCreation.Ephemeral1hInputTokens
		}
		analyses = append(analyses, analysis)
	}

	return analyses
}
