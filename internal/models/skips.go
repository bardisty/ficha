package models

// SkipDetail is one session's share of a report's skip counters. The counters
// say how much input was unreadable; this says where, so a verbose warning can
// name the session to inspect or attach to a bug report.
type SkipDetail struct {
	SessionID  string
	Unreadable bool // the session's own transcript could not be read
	Lines      int  // JSONL lines skipped as malformed or oversized, agents included
	Agents     int  // agent sub-sessions that could not be read
	// Files lists the session's transcripts that had skipped lines, its own
	// first and then its agents', so -v can say which lines and why.
	Files []FileSkips
}

// Why the parser skipped a transcript line.
const (
	SkipMalformed = "malformed" // not valid JSON
	SkipOversized = "oversized" // longer than the parser holds in memory
)

// SkippedLine is one transcript line the parser couldn't use.
type SkippedLine struct {
	Line   int // 1-indexed
	Reason string
}

// FileSkips is one transcript file's skipped lines.
type FileSkips struct {
	Path    string
	AgentID string // empty for the session's own transcript
	Count   int    // every skipped line in the file
	// Lines holds the first of them. The parser stops recording line numbers
	// past a cap, so there can be fewer than Count.
	Lines []SkippedLine
}
