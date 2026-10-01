package parser

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// A line handed out by next shares its bytes with the reader, so the next
// read is free to overwrite them. These lines mix every way a line reaches
// the caller (inside the reader's buffer, assembled across reads, shorter
// than the one before it) and each must come back whole, with nothing of an
// earlier line left in it.
func TestLineReaderReusesItsBufferAcrossLongLines(t *testing.T) {
	sizes := []int{
		3 * readerBufSize, // assembled, grows the buffer
		10,                // inside the reader's buffer
		2*readerBufSize + 7,
		readerBufSize,     // exactly one full read, then the newline alone
		readerBufSize + 1, // assembled, shorter than what the buffer held
		0,
		5 * readerBufSize,
		readerBufSize - 1,
	}
	var input strings.Builder
	want := make([]string, len(sizes))
	for i, n := range sizes {
		want[i] = strings.Repeat(string(rune('a'+i)), n)
		input.WriteString(want[i])
		input.WriteString("\n")
	}

	reader := newLineReader(strings.NewReader(input.String()))
	var kept [][]byte
	for i := range sizes {
		line, oversized, err := reader.next(maxLineBytes)
		if err != nil || oversized {
			t.Fatalf("line %d: oversized=%v err=%v", i+1, oversized, err)
		}
		if string(line) != want[i] {
			t.Fatalf("line %d: got %d bytes starting %.8q, want %d of %q",
				i+1, len(line), line, len(want[i]), want[i][:min(1, len(want[i]))])
		}
		// What a caller must do to keep a line past the next read
		kept = append(kept, bytes.Clone(line))
	}
	if _, _, err := reader.next(maxLineBytes); err != io.EOF {
		t.Fatalf("expected io.EOF after the last line, got %v", err)
	}
	for i := range kept {
		if string(kept[i]) != want[i] {
			t.Errorf("copy of line %d changed after later reads", i+1)
		}
	}
}

// The parse keeps what it decoded from each line while the reader moves on,
// which only holds if nothing decoded points into the line. Every message here
// sits on a line long enough to be assembled in the shared buffer.
func TestParseKeepsMessagesFromLongLinesIntact(t *testing.T) {
	const n = 6
	var input strings.Builder
	for i := range n {
		pad := strings.Repeat(string(rune('a'+i)), (n-i)*readerBufSize)
		fmt.Fprintf(&input, `{"type":"assistant","timestamp":"2026-09-01T10:00:%02dZ","requestId":"req_%d","message":{"id":"msg_%d","model":"model-%d","content":[{"type":"text","text":%q}],"usage":{"input_tokens":%d,"output_tokens":1}}}`+"\n",
			i, i, i, i, pad, i+1)
	}

	result, err := ParseJSONLWithResult(strings.NewReader(input.String()))
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedLines != 0 || len(result.Messages) != n {
		t.Fatalf("got %d messages and %d skipped lines, want %d and 0", len(result.Messages), result.SkippedLines, n)
	}
	for i, msg := range result.Messages {
		wantID, wantReq, wantModel := fmt.Sprintf("msg_%d", i), fmt.Sprintf("req_%d", i), fmt.Sprintf("model-%d", i)
		if msg.Message.ID != wantID || msg.RequestID != wantReq || msg.Message.Model != wantModel ||
			msg.Message.Usage.InputTokens != int64(i+1) || msg.Timestamp.Second() != i {
			t.Errorf("message %d: id=%q requestId=%q model=%q input=%d second=%d",
				i, msg.Message.ID, msg.RequestID, msg.Message.Model, msg.Message.Usage.InputTokens, msg.Timestamp.Second())
		}
	}
}

// An oversized line must leave nothing behind: not its bytes in the line
// after it, and not a buffer grown past the cap while the reader was still
// finding out how long the line was.
func TestLineReaderOversizedLineLeavesBufferBounded(t *testing.T) {
	const maxLen = 4 * readerBufSize
	fits := strings.Repeat("f", maxLen) // exactly at the cap
	over := strings.Repeat("o", 3*maxLen)
	after := strings.Repeat("a", readerBufSize+5)
	input := fits + "\r\n" + over + "\n" + after + "\n" + over + "\n" + "z\n"

	reader := newLineReader(strings.NewReader(input))
	steps := []struct {
		want      string
		oversized bool
	}{{fits, false}, {"", true}, {after, false}, {"", true}, {"z", false}}
	for i, step := range steps {
		line, oversized, err := reader.next(maxLen)
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		if oversized != step.oversized || string(line) != step.want {
			t.Fatalf("line %d: oversized=%v with %d bytes, want oversized=%v with %d bytes",
				i+1, oversized, len(line), step.oversized, len(step.want))
		}
		if limit := maxLen + len("\r\n"); cap(reader.buf) > limit {
			t.Fatalf("line %d: buffer holds %d bytes, over the %d a line at the cap needs", i+1, cap(reader.buf), limit)
		}
	}
}

// longLineTranscript builds a transcript shaped like a real one: tool results
// that are mostly small, with a few from 64 KB to 2 MB, each followed by the
// assistant line that carries usage.
func longLineTranscript(turns int) []byte {
	size := func(i int) int {
		spread := i * 7919 // a prime stride, so the sizes don't repeat in step with the cases
		switch {
		case i%200 == 199:
			return 1000000 + spread%1000000
		case i%15 == 14:
			return 64000 + spread%536000
		case i%8 == 7:
			return 5000 + spread%59000
		default:
			return 200 + spread%4800
		}
	}
	var buf bytes.Buffer
	for i := range turns {
		ts := fmt.Sprintf("2026-09-01T%02d:%02d:%02dZ", i/3600%24, i/60%60, i%60)
		fmt.Fprintf(&buf, `{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","content":"`, ts)
		buf.Write(bytes.Repeat([]byte("x"), size(i)))
		buf.WriteString(`"}]}}` + "\n")
		fmt.Fprintf(&buf, `{"type":"assistant","timestamp":%q,"requestId":"req_%d","message":{"id":"msg_%d","model":"claude-opus-5-5","usage":{"input_tokens":5,"output_tokens":300,"cache_read_input_tokens":80000}}}`+"\n", ts, i, i)
	}
	return buf.Bytes()
}

func BenchmarkParseJSONLLongLines(b *testing.B) {
	const turns = 500
	data := longLineTranscript(turns)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := ParseJSONLWithResult(bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Messages) != turns || result.SkippedLines != 0 {
			b.Fatalf("got %d messages and %d skipped lines", len(result.Messages), result.SkippedLines)
		}
	}
}

// The parse hands json.Unmarshal a line whose bytes the next read overwrites.
// That is safe only while nothing in JSONLMessage can keep those bytes:
// encoding/json copies strings, but a []byte or json.RawMessage is filled
// with the input's own bytes, and an interface field or a custom unmarshaler
// could hold on to them too.
func TestJSONLMessageCannotRetainTheLine(t *testing.T) {
	unmarshaler := reflect.TypeFor[json.Unmarshaler]()
	textUnmarshaler := reflect.TypeFor[encoding.TextUnmarshaler]()
	seen := map[reflect.Type]bool{}

	var walk func(path string, typ reflect.Type)
	walk = func(path string, typ reflect.Type) {
		if seen[typ] {
			return
		}
		seen[typ] = true
		// time.Time parses its input into numbers and keeps none of it
		if typ == reflect.TypeFor[time.Time]() {
			return
		}
		if ptr := reflect.PointerTo(typ); ptr.Implements(unmarshaler) || ptr.Implements(textUnmarshaler) {
			t.Errorf("%s (%s) has its own unmarshaler, which may keep the line's bytes", path, typ)
			return
		}
		switch typ.Kind() {
		case reflect.Interface:
			t.Errorf("%s is an interface, which may be filled with a type that keeps the line's bytes", path)
		case reflect.Slice, reflect.Array:
			if typ.Elem().Kind() == reflect.Uint8 {
				t.Errorf("%s (%s) is filled with the line's own bytes", path, typ)
				return
			}
			walk(path+"[]", typ.Elem())
		case reflect.Pointer:
			walk(path, typ.Elem())
		case reflect.Map:
			walk(path+"[key]", typ.Key())
			walk(path+"[value]", typ.Elem())
		case reflect.Struct:
			for i := range typ.NumField() {
				walk(path+"."+typ.Field(i).Name, typ.Field(i).Type)
			}
		}
	}
	walk("JSONLMessage", reflect.TypeFor[models.JSONLMessage]())
}
