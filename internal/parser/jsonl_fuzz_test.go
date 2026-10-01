package parser

import (
	"bytes"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

// jsonlFuzzSeeds starts every fuzz test that takes transcript bytes.
var jsonlFuzzSeeds = [][]byte{
	[]byte(""),
	[]byte("\n\n\n"),
	[]byte(`{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","requestId":"req_1","message":{"id":"msg_1","model":"claude-opus-4-5","usage":{"input_tokens":100,"output_tokens":50}}}`),
	[]byte(`{"type":"assistant","message":{"id":"msg_1","usage":{"input_tokens":-5,"output_tokens":9223372036854775807}}}`),
	[]byte(`{"type":"assistant","message":{"usage":{"input_tokens":1e309}}}`),
	[]byte(`{"type":"user","message":"hello"}` + "\n" + `not json at all` + "\n" + `{"type":"assistant","message":{"id":"a"}}`),
	[]byte(`{"type":"assistant","message":null}`),
	[]byte("{\"type\":\"assistant\"\x00,\"message\":{}}"),
	[]byte(`{"type":"assistant","message":{"id":"dup","usage":{}}}` + "\n" + `{"type":"assistant","message":{"id":"dup","usage":{}}}`),
	[]byte(`{"a":` + string(bytes.Repeat([]byte("["), 1000)) + `}`),
	[]byte("{\"type\":\"assistant\"}\r\n{\"type\":\"assistant\",\"message\":{}}\r\n"),
	[]byte(`{"type":"assistant","message":{"id":"m1"}}`), // no trailing newline
}

// FuzzParseJSONL feeds raw bytes to ParseJSONLWithResult, which parses
// externally-produced session files. The parser must never panic or error on
// in-memory input, and its results must satisfy basic invariants.
func FuzzParseJSONL(f *testing.F) {
	for _, s := range jsonlFuzzSeeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		result, err := ParseJSONLWithResult(bytes.NewReader(data))
		// bytes.Reader can't produce I/O errors and oversized lines are
		// skipped rather than aborting, so parsing must always succeed
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("nil result with nil error")
		}

		// SkippedAt tracks at most maxSkippedLineNumbers of the skipped lines
		if len(result.SkippedAt) > result.SkippedLines {
			t.Fatalf("len(SkippedAt)=%d > SkippedLines=%d", len(result.SkippedAt), result.SkippedLines)
		}
		if len(result.SkippedAt) > maxSkippedLineNumbers {
			t.Fatalf("len(SkippedAt)=%d exceeds cap %d", len(result.SkippedAt), maxSkippedLineNumbers)
		}

		// Skipped line numbers are 1-indexed and strictly increasing
		prev := 0
		for _, s := range result.SkippedAt {
			if s.Line <= prev {
				t.Fatalf("SkippedAt not strictly increasing: %v", result.SkippedAt)
			}
			if s.Reason != models.SkipMalformed && s.Reason != models.SkipOversized {
				t.Fatalf("SkippedAt has no reason: %v", result.SkippedAt)
			}
			prev = s.Line
		}

		// Line counts are bounded by the number of newline-separated lines
		lineCount := bytes.Count(data, []byte("\n")) + 1
		if result.SkippedLines > lineCount {
			t.Fatalf("SkippedLines=%d > line count %d", result.SkippedLines, lineCount)
		}
		if len(result.Messages) > lineCount {
			t.Fatalf("len(Messages)=%d > line count %d", len(result.Messages), lineCount)
		}

		// Only assistant messages with a message body are collected
		for i, msg := range result.Messages {
			if msg.Type != "assistant" || msg.Message == nil {
				t.Fatalf("message %d violates filter: type=%q message=%v", i, msg.Type, msg.Message)
			}
		}

		// Dedup already ran inside the parser, so it must be idempotent here
		if deduped := DeduplicateMessages(result.Messages); len(deduped) != len(result.Messages) {
			t.Fatalf("dedup not idempotent: %d -> %d messages", len(result.Messages), len(deduped))
		}
	})
}
