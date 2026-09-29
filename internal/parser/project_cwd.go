package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// cwdLineBytes caps one line of the cwd scan. Claude Code writes cwd on
	// every message line, after the message itself, so a line carrying a
	// pasted screenshot can run to megabytes; such a line is skipped and the
	// next one, which has its own cwd, answers instead.
	cwdLineBytes = 256 * 1024
	// cwdScanBytes bounds the whole read of one transcript, so a file with no
	// cwd costs a bounded read even when every line is huge.
	cwdScanBytes = 8 * 1024 * 1024
	// cwdScanFiles is how many of a project's newest transcripts are tried.
	// The newest can be empty or hold only non-message lines (a session that
	// was opened and closed), so one more try is cheap insurance.
	cwdScanFiles = 3
)

// originalPathFromTranscripts recovers a project's real directory from the
// "cwd" field of its newest transcript. Current Claude Code no longer writes
// sessions-index.json, so for most projects this is the only record of the
// path the encoded directory name stands for (the encoding is lossy: '/', '.'
// and '-' all become '-'). Returns "" when no transcript carries a cwd.
//
// There is no cache: every command discovers projects at most once per run,
// and the read usually stops within a transcript's first few lines.
func originalPathFromTranscripts(projectDir string) string {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return ""
	}

	type transcript struct {
		path    string
		modTime time.Time
	}
	var files []transcript
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, transcript{filepath.Join(projectDir, e.Name()), info.ModTime()})
	}
	// Newest first; ties by name so the pick is stable run to run.
	sort.Slice(files, func(i, j int) bool {
		if !files[i].modTime.Equal(files[j].modTime) {
			return files[i].modTime.After(files[j].modTime)
		}
		return files[i].path < files[j].path
	})

	for i, f := range files {
		if i == cwdScanFiles {
			break
		}
		if cwd := transcriptCwd(f.path); cwd != "" {
			return cwd
		}
	}
	return ""
}

// transcriptCwd returns the cwd of the first line that has one, reading at
// most cwdScanBytes. Lines over cwdLineBytes are skipped whole, and a line
// cut off by the total bound fails to parse, like any malformed line.
func transcriptCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	r := bufio.NewReaderSize(io.LimitReader(f, cwdScanBytes), 64*1024)
	for {
		line, oversized, err := readLine(r, cwdLineBytes)
		// Cheap pre-check: most lines without the key never reach the decoder
		if !oversized && bytes.Contains(line, []byte(`"cwd"`)) {
			var rec struct {
				Cwd string `json:"cwd"`
			}
			if json.Unmarshal(line, &rec) == nil && rec.Cwd != "" {
				return rec.Cwd
			}
		}
		if err != nil {
			return ""
		}
	}
}
