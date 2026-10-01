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
	// assembled chunk by chunk in lineReader's buffer, so this only sets the
	// read granularity.
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
	SkippedLines int                  // Number of lines skipped (malformed JSON or longer than maxLineBytes)
	SkippedAt    []models.SkippedLine // The skipped lines and why, capped at maxSkippedLineNumbers
	// Title is the last "ai-title" record's title. Claude Code writes a new
	// one as the session's topic shifts, so the last is the current one.
	Title string
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
	reader := newLineReader(r)

	lineNum := 0
	for {
		line, oversized, err := reader.next(maxLineBytes)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		lineNum++

		if oversized {
			// Skip just this line and keep parsing the rest of the file
			result.skip(lineNum, models.SkipOversized)
			continue
		}
		if len(line) == 0 {
			continue
		}

		var msg models.JSONLMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// Track skipped lines instead of silently ignoring
			result.skip(lineNum, models.SkipMalformed)
			continue
		}

		// Only collect assistant messages with usage data
		switch {
		case msg.Type == "assistant" && msg.Message != nil:
			result.Messages = append(result.Messages, msg)
		case msg.Type == "ai-title" && msg.AITitle != "":
			result.Title = msg.AITitle
		}
	}

	result.Messages = DeduplicateMessages(result.Messages)
	return result, nil
}

func (r *ParseResult) skip(lineNum int, reason string) {
	r.SkippedLines++
	if len(r.SkippedAt) < maxSkippedLineNumbers {
		r.SkippedAt = append(r.SkippedAt, models.SkippedLine{Line: lineNum, Reason: reason})
	}
}

// lineReader reads a transcript line by line through one buffer it keeps for
// the whole parse. Real transcripts hold many lines far longer than the
// reader's buffer, and a fresh slice for each one keeps the garbage collector
// busier than the parse itself.
type lineReader struct {
	r *bufio.Reader
	// buf assembles a line that spans several reads. It grows to the longest
	// such line seen and never past maxLen plus a line ending.
	buf []byte
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{r: bufio.NewReaderSize(r, readerBufSize)}
}

// next reads the next line, without the trailing newline. A line longer than
// maxLen is discarded through to its newline and reported with oversized=true
// so the caller can count it and continue with the next line (bufio.Scanner
// cannot do this: ErrTooLong aborts the whole scan). err is io.EOF only when
// no line remains.
//
// The returned line aliases either the bufio.Reader's buffer or lr.buf, so it
// is valid only until the next call. Callers decode it with json.Unmarshal,
// which copies every string it stores, and must not keep the slice or a
// subslice of it: a json.RawMessage or []byte field in the decoded type would
// do exactly that.
func (lr *lineReader) next(maxLen int) (line []byte, oversized bool, err error) {
	buf := lr.buf[:0]
	for {
		chunk, err := lr.r.ReadSlice('\n')
		// Measure the cap against content only. The final chunk (err == nil)
		// still carries the '\n' (and any preceding '\r'); counting it would
		// classify identical content as oversized-or-not depending purely on
		// whether the line has a trailing newline. err != nil chunks (buffer
		// full mid-line, or EOF with no terminator) carry no delimiter.
		chunkLen := len(chunk)
		if err == nil {
			chunkLen = len(trimLineEnding(chunk))
		}
		if len(buf)+chunkLen > maxLen {
			// Checked before the chunk is stored, so an oversized line never
			// grows buf past the cap on the way to being found out.
			oversized = true
		}
		switch {
		case err == nil || errors.Is(err, io.EOF): // the newline, or a final line without one
			if oversized {
				return nil, true, nil
			}
			if len(buf) == 0 {
				if len(chunk) == 0 {
					return nil, false, io.EOF
				}
				// The whole line sits in the reader's buffer
				return trimLineEnding(chunk), false, nil
			}
			lr.buf = appendChunk(buf, chunk, maxLen)
			return trimLineEnding(lr.buf), false, nil
		case errors.Is(err, bufio.ErrBufferFull): // line continues past the buffer
			if !oversized {
				buf = appendChunk(buf, chunk, maxLen)
				lr.buf = buf
			}
		default:
			return nil, false, err
		}
	}
}

// appendChunk appends chunk to buf, doubling buf when it is full but never
// past what a line of maxLen bytes and its "\r\n" need. append alone grows a
// large slice by a quarter at a time, and so overshoots the cap by as much.
func appendChunk(buf, chunk []byte, maxLen int) []byte {
	need := len(buf) + len(chunk)
	if need > cap(buf) {
		grown := max(2*cap(buf), need)
		if limit := maxLen + len("\r\n"); need <= limit {
			grown = min(grown, limit)
		}
		buf = append(make([]byte, 0, grown), buf...)
	}
	return append(buf, chunk...)
}

// trimLineEnding strips a trailing "\n" or "\r\n".
func trimLineEnding(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte("\n"))
	return bytes.TrimSuffix(line, []byte("\r"))
}

// DedupKey identifies the API response a JSONL line belongs to, so streaming
// lines of the same response can be collapsed and cross-file duplicates
// dropped. It keeps message.id and requestId in separate struct fields rather
// than concatenating them: a raw "id:requestId" string lets a colon inside an
// untrusted id alias a different id/requestId split (id "msg_1"+req "a:b"
// collides with id "msg_1:a"+req "b"), silently dropping one response's billed
// usage. Callers hold cross-file seen sets keyed by this type.
type DedupKey struct {
	ID    string
	ReqID string
}

// dedupKey returns the key for a line and whether it has one. Lines without a
// message id (ok=false) must never be collapsed together — an empty id
// identifies nothing. A missing requestId still yields a key (degraded), so two
// lines sharing an id and both missing requestId do collapse, as before.
func dedupKey(msg models.JSONLMessage) (DedupKey, bool) {
	if msg.Message == nil || msg.Message.ID == "" {
		return DedupKey{}, false
	}
	return DedupKey{ID: msg.Message.ID, ReqID: msg.RequestID}, true
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
	seenIdx := make(map[DedupKey]int)

	for _, msg := range messages {
		key, ok := dedupKey(msg)
		if !ok {
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

// ExcludeSeenMessages drops messages whose dedup key (message.id + requestId)
// is already recorded in seen — the same API response was kept from an
// earlier file — and records the keys of the messages it keeps. Claude Code's
// fork/branch flows clone the prior transcript (assistant lines included,
// with billed usage) into a new session file, so without a cross-file seen
// set aggregates bill those responses once per file. Messages without a
// message id are always kept: an empty key identifies nothing.
func ExcludeSeenMessages(messages []models.JSONLMessage, seen map[DedupKey]struct{}) []models.JSONLMessage {
	kept := make([]models.JSONLMessage, 0, len(messages))
	for _, msg := range messages {
		key, ok := dedupKey(msg)
		if !ok {
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

// ExtractUsageFromMessages extracts token usage data from parsed messages.
//
// Each usage is reconciled here, once, so every downstream consumer — cost
// calculation, token aggregation, per-TTL display — reads the same canonical
// representation instead of interpreting the raw duality independently:
//   - negative counts are clamped to zero (a corrupt line otherwise yields a
//     clamped cost next to unclamped, possibly negative, token totals)
//   - when write tokens exist, CacheCreation is non-nil and its 5m+1h buckets
//     sum exactly to CacheCreationInputTokens
func ExtractUsageFromMessages(messages []models.JSONLMessage) []models.MessageAnalysis {
	var analyses []models.MessageAnalysis

	for _, msg := range messages {
		if msg.Message == nil {
			continue
		}

		analysis := models.MessageAnalysis{
			Timestamp: msg.Timestamp,
			Model:     msg.Message.Model,
		}
		analysis.Usage, analysis.EstimatedCost = reconcileUsage(msg.Message.Usage)
		analyses = append(analyses, analysis)
	}

	return analyses
}

// reconcileUsage returns the canonical form of a raw usage: negatives clamped,
// and every cache-write token attributed to a TTL bucket. Write tokens the
// data itself doesn't attribute (no cache_creation object — the older session
// format — or a flat count exceeding the buckets' sum) are folded into the 5m
// bucket, mirroring the pricing fallback CalculateCost documents; estimated
// reports that this assumption fired, since 5m is the cheapest write tier and
// the resulting cost is a lower bound. A bucket sum exceeding the flat count
// wins over it (the same guard ContextWindowSize applies), so a sparse or
// empty cache_creation object can never zero out billed write tokens.
func reconcileUsage(usage models.TokenUsage) (reconciled models.TokenUsage, estimated bool) {
	// Sanitized deep-copies CacheCreation, so the mutations below can't touch
	// the raw JSONLMessage the usage came from.
	usage = usage.Sanitized()

	if usage.CacheCreation == nil {
		if usage.CacheCreationInputTokens > 0 {
			usage.CacheCreation = &models.CacheCreation{
				Ephemeral5mInputTokens: usage.CacheCreationInputTokens,
			}
			estimated = true
		}
		return usage, estimated
	}

	bucketSum := usage.CacheCreation.Ephemeral5mInputTokens + usage.CacheCreation.Ephemeral1hInputTokens
	if usage.CacheCreationInputTokens > bucketSum {
		usage.CacheCreation.Ephemeral5mInputTokens += usage.CacheCreationInputTokens - bucketSum
		estimated = true
	} else {
		usage.CacheCreationInputTokens = bucketSum
	}
	return usage, estimated
}
