package parser

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"
)

func TestParseJSONL(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedCount  int
		expectedModels []string
	}{
		{
			name:           "single assistant message",
			input:          `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","message":{"model":"claude-opus-4-5","usage":{"input_tokens":100,"output_tokens":50}}}`,
			expectedCount:  1,
			expectedModels: []string{"claude-opus-4-5"},
		},
		{
			name: "multiple messages",
			input: `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","message":{"model":"claude-opus-4-5","usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"assistant","timestamp":"2024-01-01T12:01:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":200,"output_tokens":100}}}`,
			expectedCount:  2,
			expectedModels: []string{"claude-opus-4-5", "claude-sonnet-4-5"},
		},
		{
			name: "filters non-assistant messages",
			input: `{"type":"user","timestamp":"2024-01-01T12:00:00Z"}
{"type":"assistant","timestamp":"2024-01-01T12:01:00Z","message":{"model":"claude-opus-4-5","usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"system","timestamp":"2024-01-01T12:02:00Z"}`,
			expectedCount:  1,
			expectedModels: []string{"claude-opus-4-5"},
		},
		{
			name:           "empty input",
			input:          "",
			expectedCount:  0,
			expectedModels: []string{},
		},
		{
			name:           "only empty lines",
			input:          "\n\n\n",
			expectedCount:  0,
			expectedModels: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages, err := ParseJSONL(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(messages) != tt.expectedCount {
				t.Errorf("message count: got %d, want %d", len(messages), tt.expectedCount)
			}
			for i, expectedModel := range tt.expectedModels {
				if i < len(messages) && messages[i].Message.Model != expectedModel {
					t.Errorf("message[%d].Model: got %s, want %s", i, messages[i].Message.Model, expectedModel)
				}
			}
		})
	}
}

func TestParseJSONLWithResult(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedMsgs    int
		expectedSkipped int
	}{
		{
			name:            "valid input no skips",
			input:           `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","message":{"model":"claude-opus-4-5","usage":{}}}`,
			expectedMsgs:    1,
			expectedSkipped: 0,
		},
		{
			name: "malformed line skipped",
			input: `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","message":{"model":"claude-opus-4-5","usage":{}}}
{invalid json}
{"type":"assistant","timestamp":"2024-01-01T12:01:00Z","message":{"model":"claude-sonnet-4-5","usage":{}}}`,
			expectedMsgs:    2,
			expectedSkipped: 1,
		},
		{
			name: "multiple malformed lines",
			input: `not json
{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","message":{"model":"claude-opus-4-5","usage":{}}}
also not json
{truncated`,
			expectedMsgs:    1,
			expectedSkipped: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseJSONLWithResult(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Messages) != tt.expectedMsgs {
				t.Errorf("message count: got %d, want %d", len(result.Messages), tt.expectedMsgs)
			}
			if result.SkippedLines != tt.expectedSkipped {
				t.Errorf("skipped lines: got %d, want %d", result.SkippedLines, tt.expectedSkipped)
			}
			if result.SkippedLines != len(result.SkippedAt) {
				t.Errorf("SkippedAt length mismatch: got %d, want %d", len(result.SkippedAt), result.SkippedLines)
			}
		})
	}
}

// An oversized line (over maxLineBytes) in the MIDDLE of the file must be
// skipped on its own, with every following line still parsed — a reader that
// stops at the first oversized line silently drops the rest of the session.
func TestParseJSONLWithResult_OversizedLineMiddle(t *testing.T) {
	line1 := `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","message":{"id":"msg_1","model":"claude-opus-4-5","usage":{"input_tokens":100,"output_tokens":50}}}`
	line3 := `{"type":"assistant","timestamp":"2024-01-01T12:01:00Z","message":{"id":"msg_2","model":"claude-opus-4-5","usage":{"input_tokens":200,"output_tokens":75}}}`

	var input strings.Builder
	input.Grow(maxLineBytes + len(line1) + len(line3) + 16)
	input.WriteString(line1)
	input.WriteString("\n")
	input.WriteString(strings.Repeat("x", maxLineBytes+1))
	input.WriteString("\n")
	input.WriteString(line3)
	input.WriteString("\n")

	result, err := ParseJSONLWithResult(strings.NewReader(input.String()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages (lines after the oversized one must not be dropped), got %d", len(result.Messages))
	}
	if result.Messages[0].Message.ID != "msg_1" || result.Messages[1].Message.ID != "msg_2" {
		t.Errorf("unexpected message ids: %s, %s", result.Messages[0].Message.ID, result.Messages[1].Message.ID)
	}
	if result.SkippedLines != 1 {
		t.Errorf("expected 1 skipped line, got %d", result.SkippedLines)
	}
	if len(result.SkippedAt) != 1 || result.SkippedAt[0] != 2 {
		t.Errorf("expected SkippedAt=[2], got %v", result.SkippedAt)
	}
}

// Lines between the old 10MB scanner cap and maxLineBytes (e.g. embedded
// base64 images) must parse normally now.
func TestParseJSONLWithResult_LargeLineWithinCap(t *testing.T) {
	filler := strings.Repeat("a", 11*1024*1024) // >10MB of payload inside valid JSON
	bigLine := `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","requestId":"req_1","message":{"id":"msg_big","model":"claude-opus-4-5","usage":{"input_tokens":100,"output_tokens":50}},"filler":"` + filler + `"}`

	result, err := ParseJSONLWithResult(strings.NewReader(bigLine))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SkippedLines != 0 {
		t.Errorf("expected 0 skipped lines, got %d", result.SkippedLines)
	}
	if len(result.Messages) != 1 || result.Messages[0].Message.ID != "msg_big" {
		t.Fatalf("expected the 11MB line to parse, got %d messages", len(result.Messages))
	}
}

// readLine is exercised directly with a small cap so the skip-and-continue
// logic gets thorough coverage without allocating >50MB per case.
func TestReadLine(t *testing.T) {
	const maxLen = 16
	tests := []struct {
		name  string
		input string
		// expected sequence of (line, oversized) results, in order
		lines     []string
		oversized []bool
	}{
		{
			name:      "normal lines",
			input:     "aaa\nbbb\n",
			lines:     []string{"aaa", "bbb"},
			oversized: []bool{false, false},
		},
		{
			name:      "no trailing newline",
			input:     "aaa\nbbb",
			lines:     []string{"aaa", "bbb"},
			oversized: []bool{false, false},
		},
		{
			name:      "crlf stripped",
			input:     "aaa\r\nbbb\r\n",
			lines:     []string{"aaa", "bbb"},
			oversized: []bool{false, false},
		},
		{
			name:      "empty lines preserved",
			input:     "\naaa\n\n",
			lines:     []string{"", "aaa", ""},
			oversized: []bool{false, false, false},
		},
		{
			name:      "oversized line in middle",
			input:     "aaa\n" + strings.Repeat("x", maxLen*3) + "\nbbb\n",
			lines:     []string{"aaa", "", "bbb"},
			oversized: []bool{false, true, false},
		},
		{
			name:      "oversized line at EOF without newline",
			input:     "aaa\n" + strings.Repeat("x", maxLen*3),
			lines:     []string{"aaa", ""},
			oversized: []bool{false, true},
		},
		{
			name:      "consecutive oversized lines",
			input:     strings.Repeat("x", maxLen*2) + "\n" + strings.Repeat("y", maxLen*2) + "\nccc\n",
			lines:     []string{"", "", "ccc"},
			oversized: []bool{true, true, false},
		},
		{
			name:      "line exactly at cap",
			input:     strings.Repeat("x", maxLen-1) + "\n", // maxLen-1 content + newline = maxLen bytes
			lines:     []string{strings.Repeat("x", maxLen-1)},
			oversized: []bool{false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Small reader buffer forces the ErrBufferFull accumulation path
			reader := bufio.NewReaderSize(strings.NewReader(tt.input), 16)
			for i := range tt.lines {
				line, oversized, err := readLine(reader, maxLen)
				if err != nil {
					t.Fatalf("line %d: unexpected error: %v", i+1, err)
				}
				if oversized != tt.oversized[i] {
					t.Errorf("line %d: oversized = %v, want %v", i+1, oversized, tt.oversized[i])
				}
				if string(line) != tt.lines[i] {
					t.Errorf("line %d: got %q, want %q", i+1, string(line), tt.lines[i])
				}
			}
			if _, _, err := readLine(reader, maxLen); err != io.EOF {
				t.Errorf("expected io.EOF after last line, got %v", err)
			}
		})
	}
}

func TestExtractUsageReconcilesCacheCreation(t *testing.T) {
	// CacheCreation present but CacheCreationInputTokens is 0 in JSON
	input := `{"type":"assistant","timestamp":"2024-01-15T10:30:00Z","message":{"model":"claude-opus-4-5","usage":{"input_tokens":1000,"output_tokens":500,"cache_creation":{"ephemeral_5m_input_tokens":300,"ephemeral_1h_input_tokens":400}}}}`

	messages, err := ParseJSONL(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	analyses := ExtractUsageFromMessages(messages)
	if len(analyses) != 1 {
		t.Fatalf("expected 1 analysis, got %d", len(analyses))
	}

	a := analyses[0]
	if a.Usage.CacheCreationInputTokens != 700 {
		t.Errorf("CacheCreationInputTokens: got %d, want 700 (reconciled from CacheCreation)", a.Usage.CacheCreationInputTokens)
	}
}

func TestExtractUsageFromMessages(t *testing.T) {
	input := `{"type":"assistant","timestamp":"2024-01-15T10:30:00Z","message":{"model":"claude-opus-4-5","usage":{"input_tokens":1000,"output_tokens":500,"cache_read_input_tokens":100}}}`

	messages, err := ParseJSONL(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	analyses := ExtractUsageFromMessages(messages)
	if len(analyses) != 1 {
		t.Fatalf("expected 1 analysis, got %d", len(analyses))
	}

	a := analyses[0]
	if a.Model != "claude-opus-4-5" {
		t.Errorf("Model: got %s, want claude-opus-4-5", a.Model)
	}
	if a.Usage.InputTokens != 1000 {
		t.Errorf("InputTokens: got %d, want 1000", a.Usage.InputTokens)
	}
	if a.Usage.OutputTokens != 500 {
		t.Errorf("OutputTokens: got %d, want 500", a.Usage.OutputTokens)
	}
	if a.Usage.CacheReadInputTokens != 100 {
		t.Errorf("CacheReadInputTokens: got %d, want 100", a.Usage.CacheReadInputTokens)
	}

	expectedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !a.Timestamp.Equal(expectedTime) {
		t.Errorf("Timestamp: got %v, want %v", a.Timestamp, expectedTime)
	}
}

// === Deduplication Tests ===

// TestParseJSONLDeduplicatesStreamingLines mirrors real Claude Code session
// files: the same assistant message (same message.id + requestId) is written
// once per streaming chunk with output_tokens growing across lines. Only the
// last line carries the final billed usage.
func TestParseJSONLDeduplicatesStreamingLines(t *testing.T) {
	input := `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","requestId":"req_A","message":{"id":"msg_A","model":"claude-opus-4-5","usage":{"input_tokens":100,"output_tokens":5,"cache_creation_input_tokens":800,"cache_read_input_tokens":9000}}}
{"type":"assistant","timestamp":"2024-01-01T12:00:02Z","requestId":"req_A","message":{"id":"msg_A","model":"claude-opus-4-5","usage":{"input_tokens":100,"output_tokens":120,"cache_creation_input_tokens":800,"cache_read_input_tokens":9000}}}
{"type":"assistant","timestamp":"2024-01-01T12:00:05Z","requestId":"req_A","message":{"id":"msg_A","model":"claude-opus-4-5","usage":{"input_tokens":100,"output_tokens":394,"cache_creation_input_tokens":800,"cache_read_input_tokens":9000}}}
{"type":"assistant","timestamp":"2024-01-01T12:01:00Z","requestId":"req_B","message":{"id":"msg_B","model":"claude-opus-4-5","usage":{"input_tokens":50,"output_tokens":10}}}`

	messages, err := ParseJSONL(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("message count: got %d, want 2 (4 lines, 2 distinct message ids)", len(messages))
	}

	// First message keeps its position but carries the LAST line's usage
	first := messages[0]
	if first.Message.ID != "msg_A" {
		t.Errorf("messages[0].ID: got %s, want msg_A", first.Message.ID)
	}
	if first.Message.Usage.OutputTokens != 394 {
		t.Errorf("messages[0].OutputTokens: got %d, want 394 (final streaming line)", first.Message.Usage.OutputTokens)
	}
	expectedTime := time.Date(2024, 1, 1, 12, 0, 5, 0, time.UTC)
	if !first.Timestamp.Equal(expectedTime) {
		t.Errorf("messages[0].Timestamp: got %v, want %v (last line's timestamp)", first.Timestamp, expectedTime)
	}

	if messages[1].Message.ID != "msg_B" {
		t.Errorf("messages[1].ID: got %s, want msg_B", messages[1].Message.ID)
	}
}

func TestParseJSONLDedupKeyHandling(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedCount int
	}{
		{
			name: "missing message id never collapses",
			input: `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","message":{"model":"claude-opus-4-5","usage":{"output_tokens":10}}}
{"type":"assistant","timestamp":"2024-01-01T12:01:00Z","message":{"model":"claude-opus-4-5","usage":{"output_tokens":20}}}
{"type":"assistant","timestamp":"2024-01-01T12:02:00Z","message":{"model":"claude-opus-4-5","usage":{"output_tokens":30}}}`,
			expectedCount: 3,
		},
		{
			name: "empty message id never collapses",
			input: `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","message":{"id":"","model":"claude-opus-4-5","usage":{"output_tokens":10}}}
{"type":"assistant","timestamp":"2024-01-01T12:01:00Z","message":{"id":"","model":"claude-opus-4-5","usage":{"output_tokens":20}}}`,
			expectedCount: 2,
		},
		{
			name: "same id different requestId kept separate",
			input: `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","requestId":"req_A","message":{"id":"msg_A","model":"claude-opus-4-5","usage":{"output_tokens":10}}}
{"type":"assistant","timestamp":"2024-01-01T12:01:00Z","requestId":"req_B","message":{"id":"msg_A","model":"claude-opus-4-5","usage":{"output_tokens":20}}}`,
			expectedCount: 2,
		},
		{
			name: "same id missing requestId still collapses",
			input: `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","message":{"id":"msg_A","model":"claude-opus-4-5","usage":{"output_tokens":10}}}
{"type":"assistant","timestamp":"2024-01-01T12:01:00Z","message":{"id":"msg_A","model":"claude-opus-4-5","usage":{"output_tokens":20}}}`,
			expectedCount: 1,
		},
		{
			name: "interleaved duplicates collapse to first-seen order",
			input: `{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","requestId":"req_A","message":{"id":"msg_A","model":"claude-opus-4-5","usage":{"output_tokens":5}}}
{"type":"assistant","timestamp":"2024-01-01T12:00:01Z","requestId":"req_B","message":{"id":"msg_B","model":"claude-opus-4-5","usage":{"output_tokens":7}}}
{"type":"assistant","timestamp":"2024-01-01T12:00:02Z","requestId":"req_A","message":{"id":"msg_A","model":"claude-opus-4-5","usage":{"output_tokens":50}}}`,
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages, err := ParseJSONL(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(messages) != tt.expectedCount {
				t.Errorf("message count: got %d, want %d", len(messages), tt.expectedCount)
			}
		})
	}
}

func TestDeduplicateMessagesPassthrough(t *testing.T) {
	if got := DeduplicateMessages(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}

	single, err := ParseJSONL(strings.NewReader(`{"type":"assistant","timestamp":"2024-01-01T12:00:00Z","message":{"id":"msg_A","model":"claude-opus-4-5","usage":{"output_tokens":10}}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := DeduplicateMessages(single); len(got) != 1 {
		t.Errorf("single message: got %d messages, want 1", len(got))
	}
}
