package parser

import (
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

func TestParseResultWarning(t *testing.T) {
	tests := []struct {
		name      string
		result    ParseResult
		wantEmpty bool
		contains  string
	}{
		{
			name:      "no skipped lines",
			result:    ParseResult{SkippedLines: 0},
			wantEmpty: true,
		},
		{
			name:     "one skipped line",
			result:   ParseResult{SkippedLines: 1, SkippedAt: []int{5}},
			contains: "1 malformed line",
		},
		{
			name:     "multiple skipped lines",
			result:   ParseResult{SkippedLines: 3, SkippedAt: []int{2, 5, 8}},
			contains: "3 malformed lines",
		},
		{
			name:     "many skipped lines truncated",
			result:   ParseResult{SkippedLines: 10, SkippedAt: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
			contains: "and 5 more",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warning := tt.result.Warning()
			if tt.wantEmpty && warning != "" {
				t.Errorf("expected empty warning, got %q", warning)
			}
			if !tt.wantEmpty && !strings.Contains(warning, tt.contains) {
				t.Errorf("warning %q does not contain %q", warning, tt.contains)
			}
		})
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
