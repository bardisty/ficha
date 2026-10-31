package parser

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"time"
)

// tailSize is how much of a transcript's end NewestTailTimestamp and
// NewestReplyTimestamp read.
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
	lines, _ := tailLines(f, size)
	for _, line := range lines {
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

// NewestReplyTimestamp returns the latest timestamp among the assistant
// replies in the transcript at path. Claude Code keeps appending records that
// aren't replies, such as system lines, long after a session's last reply, so
// the file's mtime can't stand in for it.
//
// It reads the last 64 KB, and the whole file only when no reply there has a
// timestamp. ok is false when the file holds no reply with one, or can't be
// read.
func NewestReplyTimestamp(path string) (newest time.Time, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return time.Time{}, false
	}
	lines, whole := tailLines(f, info.Size())
	for _, line := range lines {
		newest, ok = laterReply(line, newest, ok)
	}
	if ok || whole {
		return newest, ok
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return time.Time{}, false
	}
	reader := newLineReader(f)
	for {
		line, oversized, err := reader.next(maxLineBytes)
		if err != nil {
			return newest, ok
		}
		if !oversized {
			newest, ok = laterReply(line, newest, ok)
		}
	}
}

// laterReply returns line's timestamp when line is an assistant reply the
// parse would keep and is later than newest, and newest otherwise.
func laterReply(line []byte, newest time.Time, ok bool) (time.Time, bool) {
	if len(line) == 0 || droppable(line) {
		return newest, ok
	}
	var entry struct {
		Type      string           `json:"type"`
		Message   *json.RawMessage `json:"message"`
		Timestamp time.Time        `json:"timestamp"`
	}
	if json.Unmarshal(line, &entry) != nil || entry.Type != "assistant" || entry.Message == nil || entry.Timestamp.IsZero() {
		return newest, ok
	}
	if !ok || entry.Timestamp.After(newest) {
		return entry.Timestamp, true
	}
	return newest, ok
}

// tailLines splits the last 64 KB of f, which is size bytes long, into lines,
// less the partial line it starts inside. whole is true when that covers the
// file. A last line longer than the tail leaves no line at all.
func tailLines(f *os.File, size int64) (lines [][]byte, whole bool) {
	// One byte more than the tail says whether the tail starts on a line
	// boundary. Whatever precedes the first newline is the end of a line that
	// began earlier, or that one byte.
	start := max(size-tailSize-1, 0)
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil, false
	}
	if start > 0 {
		cut := bytes.IndexByte(buf, '\n')
		if cut < 0 {
			return nil, false
		}
		buf = buf[cut+1:]
	}
	return bytes.Split(buf, []byte{'\n'}), start == 0
}
