package parser

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

// kept is the parse's own rule for which decoded records it holds on to.
func kept(msg models.JSONLMessage) bool {
	return (msg.Type == "assistant" && msg.Message != nil) || (msg.Type == "ai-title" && msg.AITitle != "")
}

// scanCases are lines the scan could get wrong, each with whether it is
// valid JSON and, when it is, whether the scan may drop it undecoded.
var scanCases = []struct {
	name  string
	line  string
	valid bool
	drop  bool
}{
	// What a transcript is mostly made of
	{"user line", `{"type":"user","message":{"role":"user","content":"hi"}}`, true, true},
	{"no type at all", `{"summary":"x","leafUuid":"y"}`, true, true},
	{"empty object", `{}`, true, true},
	{"spaced out", " {\t\"type\" : \"user\" , \"a\" : [ 1 , 2 ] } \r", true, true},
	{"assistant", `{"type":"assistant","message":{"id":"m"}}`, true, false},
	{"assistant without a message", `{"type":"assistant"}`, true, false},
	{"ai-title", `{"type":"ai-title","aiTitle":"Fix the parser"}`, true, false},

	// Only the line's own type counts
	{"type nested in message", `{"type":"user","message":{"type":"assistant"}}`, true, true},
	{"type nested in content", `{"type":"user","message":{"content":[{"type":"assistant"}]}}`, true, true},
	{"assistant as some other key's value", `{"type":"user","kind":"assistant"}`, true, true},
	{"assistant as a key", `{"assistant":"type","type":"user"}`, true, true},

	// encoding/json matches keys without regard to case
	{"upper-case key", `{"TYPE":"assistant"}`, true, false},
	{"mixed-case key", `{"tYpE":"ai-title"}`, true, false},
	{"upper-case key, dropped type", `{"TYPE":"user"}`, true, true},
	{"value case matters", `{"type":"Assistant"}`, true, true},
	{"escaped key", `{"ty\u0070e":"assistant"}`, true, false},
	{"escaped key that isn't type", `{"a\nb":1,"type":"user"}`, true, false},
	{"non-ASCII key", "{\"t\u00ffpe\":\"x\",\"type\":\"user\"}", true, false},
	{"escaped value", `{"type":"assist\u0061nt"}`, true, false},
	{"escaped value that isn't kept", `{"type":"us\u0065r"}`, true, false},

	// The last of a repeated key wins, except that a null changes nothing
	{"user then assistant", `{"type":"user","type":"assistant"}`, true, false},
	{"assistant then user", `{"type":"assistant","type":"user"}`, true, false},
	{"assistant then null", `{"type":"assistant","type":null}`, true, false},
	{"user then upper-case assistant", `{"type":"user","TYPE":"assistant"}`, true, false},
	{"user twice", `{"type":"user","type":"user"}`, true, false},

	// A type that isn't a string, and lines that aren't objects
	{"number type", `{"type":5}`, true, false},
	{"null type", `{"type":null}`, true, false},
	{"object type", `{"type":{"type":"user"}}`, true, false},
	{"array", `[{"type":"user"}]`, true, false},
	{"null", `null`, true, false},
	{"number", `12`, true, false},
	{"string", `"assistant"`, true, false},

	// Valid by encoding/json's rules, if not by everyone's
	{"invalid UTF-8 in a string", "{\"type\":\"user\",\"a\":\"\xff\xfe\"}", true, true},
	{"invalid UTF-8 in the type", "{\"type\":\"assistant\xff\"}", true, true},
	{"lone surrogate escape", `{"type":"user","a":"\ud800"}`, true, true},
	{"every escape", `{"type":"user","a":"\"\\\/\b\f\n\r\t\u00e9"}`, true, true},
	{"numbers", `{"type":"user","a":[0,-0,1.5,-1.5e10,2E-3,3e+4,0.0]}`, true, true},
	{"literals", `{"type":"user","a":[true,false,null]}`, true, true},
	{"DEL in a string", "{\"type\":\"user\",\"a\":\"\x7f\"}", true, true},

	// Invalid: each of these must still reach the decoder and count
	{"empty", ``, false, false},
	{"spaces only", `   `, false, false},
	{"not json", `not json at all`, false, false},
	{"cut off", `{"type":"user","timestamp":`, false, false},
	{"unclosed string", `{"type":"user`, false, false},
	{"unclosed object", `{"type":"user"`, false, false},
	{"trailing comma in object", `{"type":"user",}`, false, false},
	{"trailing comma in array", `{"type":"user","a":[1,]}`, false, false},
	{"missing colon", `{"type" "user"}`, false, false},
	{"bare key", `{type:"user"}`, false, false},
	{"single quotes", `{'type':'user'}`, false, false},
	{"trailing garbage", `{"type":"user"}x`, false, false},
	{"two values", `{"type":"user"}{"type":"user"}`, false, false},
	{"NUL between tokens", "{\"type\":\"user\"\x00}", false, false},
	{"control byte in a string", "{\"type\":\"user\",\"a\":\"\x01\"}", false, false},
	{"tab in a string", "{\"type\":\"user\",\"a\":\"\t\"}", false, false},
	{"bad escape", `{"type":"user","a":"\x"}`, false, false},
	{"short unicode escape", `{"type":"user","a":"\u12"}`, false, false},
	{"non-hex unicode escape", `{"type":"user","a":"\u12g4"}`, false, false},
	{"escape at the end", `{"type":"user","a":"\`, false, false},
	{"leading zero", `{"type":"user","a":01}`, false, false},
	{"bare minus", `{"type":"user","a":-}`, false, false},
	{"leading plus", `{"type":"user","a":+1}`, false, false},
	{"no digits after the point", `{"type":"user","a":1.}`, false, false},
	{"no digits before the point", `{"type":"user","a":.5}`, false, false},
	{"no exponent digits", `{"type":"user","a":1e}`, false, false},
	{"signed empty exponent", `{"type":"user","a":1e+}`, false, false},
	{"misspelled literal", `{"type":"user","a":tru}`, false, false},
	{"literal run on", `{"type":"user","a":nullx}`, false, false},
	{"upper-case literal", `{"type":"user","a":True}`, false, false},
	{"mismatched brackets", `{"type":"user","a":[}]`, false, false},
}

func TestScanLine(t *testing.T) {
	for _, tt := range scanCases {
		t.Run(tt.name, func(t *testing.T) {
			line := []byte(tt.line)
			if got := json.Valid(line); got != tt.valid {
				t.Fatalf("the case is wrong: json.Valid = %v, want %v", got, tt.valid)
			}
			valid, _ := scanLine(line)
			if valid != tt.valid {
				t.Errorf("valid = %v, want %v", valid, tt.valid)
			}
			if got := droppable(line); got != tt.drop {
				t.Errorf("droppable = %v, want %v", got, tt.drop)
			}
			if tt.drop {
				var msg models.JSONLMessage
				_ = json.Unmarshal(line, &msg)
				if kept(msg) {
					t.Errorf("the case is wrong: the parse keeps this line (type %q)", msg.Type)
				}
			}
		})
	}
}

// encoding/json gives up on input nested deeper than 10,000, and the scan
// has to give up at the same depth.
func TestScanLineNestingLimit(t *testing.T) {
	nested := func(open, shut string, depth int) []byte {
		return []byte(`{"type":"user","a":` + strings.Repeat(open, depth) + "1" + strings.Repeat(shut, depth) + `}`)
	}
	for _, depth := range []int{maxJSONDepth - 2, maxJSONDepth - 1, maxJSONDepth, maxJSONDepth + 1} {
		for _, line := range [][]byte{nested("[", "]", depth), nested(`{"a":`, "}", depth)} {
			want := json.Valid(line)
			if valid, _ := scanLine(line); valid != want {
				t.Errorf("%d levels inside the line's object, starting %.12s: valid = %v, json.Valid = %v", depth, line, valid, want)
			}
		}
	}
	if !json.Valid(nested("[", "]", maxJSONDepth-1)) || json.Valid(nested("[", "]", maxJSONDepth)) {
		t.Error("encoding/json's limit isn't where this test assumes, so it no longer straddles it")
	}
}

// A valid line the parse has no use for stops counting as skipped when one
// of its fields has the wrong type, since it is never decoded. It holds no
// usage, so the warning about it was a false alarm. An assistant line with
// the same fault still counts, and so does every line that isn't valid JSON.
func TestParseDoesNotCountMistypedLinesItDrops(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"user","timestamp":"yesterday","message":{"role":"user"}}`,
		`{"type":"user","message":"hello"}`,
		`{"type":"assistant","timestamp":"yesterday","message":{"id":"m1"}}`,
		`{"type":"user","timestamp":`,
		`{"type":"assistant","timestamp":"2026-09-01T10:00:00Z","message":{"id":"m2"}}`,
	}, "\n")

	result, err := ParseJSONLWithResult(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := []models.SkippedLine{{Line: 3, Reason: models.SkipMalformed}, {Line: 4, Reason: models.SkipMalformed}}
	if result.SkippedLines != len(want) || !reflect.DeepEqual(result.SkippedAt, want) {
		t.Errorf("skipped %d lines at %v, want %v", result.SkippedLines, result.SkippedAt, want)
	}
	if len(result.Messages) != 1 || result.Messages[0].Message.ID != "m2" {
		t.Errorf("got %d messages, want only m2", len(result.Messages))
	}
}

func addScanSeeds(f *testing.F) {
	for _, s := range jsonlFuzzSeeds {
		f.Add(s)
	}
	for _, c := range scanCases {
		f.Add([]byte(c.line))
	}
}

// FuzzScanLineAgreesWithJSONValid holds the scan to encoding/json on what
// counts as valid. If the scan accepted more, a malformed line would stop
// being counted; if it accepted less, nothing would be dropped that should
// be, which costs only time.
func FuzzScanLineAgreesWithJSONValid(f *testing.F) {
	addScanSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		want := json.Valid(data)
		if valid, _ := scanLine(data); valid != want {
			t.Fatalf("scanLine says valid=%v, json.Valid says %v, for %q", valid, want, data)
		}
	})
}

// decodeEveryLine is the parse without the scan: every line goes through
// json.Unmarshal, and every line it fails on counts as malformed.
func decodeEveryLine(data []byte) *ParseResult {
	result := &ParseResult{}
	reader := newLineReader(bytes.NewReader(data))
	for lineNum := 1; ; lineNum++ {
		line, oversized, err := reader.next(maxLineBytes)
		if err != nil {
			break
		}
		if oversized {
			result.skip(lineNum, models.SkipOversized)
			continue
		}
		if len(line) == 0 {
			continue
		}
		var msg models.JSONLMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			result.skip(lineNum, models.SkipMalformed)
			continue
		}
		switch {
		case msg.Type == "assistant" && msg.Message != nil:
			result.Messages = append(result.Messages, msg)
		case msg.Type == "ai-title" && msg.AITitle != "":
			result.Title = msg.AITitle
		}
	}
	result.Messages = DeduplicateMessages(result.Messages)
	return result
}

// FuzzDroppedLinesAreNeverKept checks the scan from both ends. No line it
// drops may decode to a record the parse keeps. And a whole transcript must
// parse to the same messages and title as it does when every line is
// decoded, with the same skipped lines except for valid ones the scan drops.
func FuzzDroppedLinesAreNeverKept(f *testing.F) {
	addScanSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		lines := bytes.Split(data, []byte("\n"))
		for _, line := range lines {
			if !droppable(line) {
				continue
			}
			var msg models.JSONLMessage
			_ = json.Unmarshal(line, &msg)
			if kept(msg) || msg.Type == "assistant" || msg.Type == "ai-title" {
				t.Fatalf("dropped a line that decodes to type %q: %q", msg.Type, line)
			}
		}
		// SkippedAt holds only the first skips, so past that the two
		// results can't be lined up
		if len(lines) > maxSkippedLineNumbers {
			return
		}

		got, err := ParseJSONLWithResult(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		want := decodeEveryLine(data)
		if !reflect.DeepEqual(got.Messages, want.Messages) || got.Title != want.Title {
			t.Fatalf("parse differs from decoding every line, for %q\n got: %+v\nwant: %+v", data, got, want)
		}
		counted := map[models.SkippedLine]bool{}
		for _, skip := range got.SkippedAt {
			counted[skip] = true
		}
		uncounted := 0
		for _, skip := range want.SkippedAt {
			if counted[skip] {
				continue
			}
			uncounted++
			if line := bytes.TrimSuffix(lines[skip.Line-1], []byte("\r")); !droppable(line) {
				t.Fatalf("line %d is no longer counted as %s, and the scan didn't drop it: %q", skip.Line, skip.Reason, line)
			}
		}
		if got.SkippedLines != want.SkippedLines-uncounted {
			t.Fatalf("skipped %d lines, want %d less the %d valid ones the scan drops, for %q", got.SkippedLines, want.SkippedLines, uncounted, data)
		}
	})
}

func BenchmarkScanLine(b *testing.B) {
	data := longLineTranscript(500)
	lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		for _, line := range lines {
			if valid, _ := scanLine(line); !valid {
				b.Fatal("invalid line in the benchmark's transcript")
			}
		}
	}
}
