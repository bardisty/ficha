package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/bardisty/ficha/internal/models"
)

const (
	// maxSkippedLineNumbers is the maximum number of skipped line numbers to track.
	// This prevents unbounded memory growth on extremely malformed files.
	maxSkippedLineNumbers = 100

	// readerBufSize is the bufio.Reader buffer size (64KB). Longer lines are
	// accumulated chunk by chunk, so this only sets the read granularity.
	readerBufSize = 64 * 1024

	// maxLineBytes caps how many bytes of a single JSONL line are held in
	// memory (50MB). Lines with embedded base64 images can exceed 10MB, so the
	// cap is generous; a line over it is skipped (counted in ParseResult) and
	// parsing continues with the next line.
	maxLineBytes = 50 * 1024 * 1024
)

// ParseResult contains the parsed messages and any parse warnings
type ParseResult struct {
	Messages     []models.JSONLMessage
	SkippedLines int   // Number of lines skipped (malformed JSON or longer than maxLineBytes)
	SkippedAt    []int // Line numbers of skipped lines (1-indexed, capped at maxSkippedLineNumbers)
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
	reader := bufio.NewReaderSize(r, readerBufSize)

	lineNum := 0
	for {
		line, oversized, err := readLine(reader, maxLineBytes)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		lineNum++

		if oversized {
			// Skip just this line and keep parsing the rest of the file
			result.SkippedLines++
			if len(result.SkippedAt) < maxSkippedLineNumbers {
				result.SkippedAt = append(result.SkippedAt, lineNum)
			}
			continue
		}
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

	result.Messages = DeduplicateMessages(result.Messages)
	return result, nil
}

// readLine reads the next line from r, without the trailing newline. A line
// longer than maxLen is discarded through to its newline and reported with
// oversized=true so the caller can count it and continue with the next line
// (bufio.Scanner cannot do this: ErrTooLong aborts the whole scan).
// err is io.EOF only when no line remains.
func readLine(r *bufio.Reader, maxLen int) (line []byte, oversized bool, err error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if !oversized && len(buf)+len(chunk) > maxLen {
			oversized = true
			buf = nil
		}
		if !oversized {
			buf = append(buf, chunk...)
		}
		switch {
		case err == nil: // reached the newline
			if oversized {
				return nil, true, nil
			}
			return trimLineEnding(buf), false, nil
		case errors.Is(err, bufio.ErrBufferFull): // line continues past the buffer
			continue
		case errors.Is(err, io.EOF):
			if oversized {
				return nil, true, nil
			}
			if len(buf) == 0 {
				return nil, false, io.EOF
			}
			return trimLineEnding(buf), false, nil // final line without newline
		default:
			return nil, false, err
		}
	}
}

// trimLineEnding strips a trailing "\n" or "\r\n".
func trimLineEnding(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte("\n"))
	return bytes.TrimSuffix(line, []byte("\r"))
}

// dedupKey identifies the API response a JSONL line belongs to, so streaming
// lines of the same response can be collapsed. Returns "" for lines without a
// message id — those must never be collapsed together.
func dedupKey(msg models.JSONLMessage) string {
	if msg.Message == nil || msg.Message.ID == "" {
		return ""
	}
	return msg.Message.ID + ":" + msg.RequestID
}

// DeduplicateMessages collapses repeated streaming lines of the same API
// response (same message.id + requestId). Claude Code writes an assistant
// message once per streaming chunk with usage that grows across lines; only
// the last line carries the final billed usage, so the last occurrence wins.
// Order follows first appearance. Lines without a message id are kept as-is.
func DeduplicateMessages(messages []models.JSONLMessage) []models.JSONLMessage {
	if len(messages) < 2 {
		return messages
	}

	deduped := make([]models.JSONLMessage, 0, len(messages))
	seenIdx := make(map[string]int)

	for _, msg := range messages {
		key := dedupKey(msg)
		if key == "" {
			deduped = append(deduped, msg)
			continue
		}
		if idx, ok := seenIdx[key]; ok {
			deduped[idx] = msg
			continue
		}
		seenIdx[key] = len(deduped)
		deduped = append(deduped, msg)
	}

	return deduped
}

// ExcludeSeenMessages drops messages whose dedup key (message.id:requestId)
// is already recorded in seen — the same API response was kept from an
// earlier file — and records the keys of the messages it keeps. Claude Code's
// fork/branch flows clone the prior transcript (assistant lines included,
// with billed usage) into a new session file, so without a cross-file seen
// set aggregates bill those responses once per file. Messages without a
// message id are always kept: an empty key identifies nothing.
func ExcludeSeenMessages(messages []models.JSONLMessage, seen map[string]struct{}) []models.JSONLMessage {
	kept := make([]models.JSONLMessage, 0, len(messages))
	for _, msg := range messages {
		key := dedupKey(msg)
		if key == "" {
			kept = append(kept, msg)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		kept = append(kept, msg)
	}
	return kept
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
