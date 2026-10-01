package parser

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"time"
)

// tailSize is how much of a transcript's end NewestTailTimestamp reads.
const tailSize = 64 * 1024

// NewestTailTimestamp returns the latest timestamp among the complete lines
// in the last 64 KB of f, which is size bytes long. Lines aren't always in
// time order, so it is the latest of all of them, not the last line's.
//
// ok is false when that tail holds no line with a timestamp: the file is
// empty, its last line is longer than the tail, or none of the lines there
// carries one. The caller then has to parse the file to learn anything
// about its times.
func NewestTailTimestamp(f *os.File, size int64) (newest time.Time, ok bool) {
	// One byte more than the tail says whether the tail starts on a line
	// boundary. Whatever precedes the first newline is the end of a line that
	// began earlier, or that one byte.
	start := max(size-tailSize-1, 0)
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return time.Time{}, false
	}
	if start > 0 {
		cut := bytes.IndexByte(buf, '\n')
		if cut < 0 {
			return time.Time{}, false
		}
		buf = buf[cut+1:]
	}
	for _, line := range bytes.Split(buf, []byte{'\n'}) {
		// A line cut short, such as one still being written, fails to decode.
		var entry struct {
			Timestamp time.Time `json:"timestamp"`
		}
		if json.Unmarshal(line, &entry) != nil || entry.Timestamp.IsZero() {
			continue
		}
		if !ok || entry.Timestamp.After(newest) {
			newest, ok = entry.Timestamp, true
		}
	}
	return newest, ok
}
