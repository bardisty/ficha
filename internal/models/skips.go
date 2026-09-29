package models

// SkipDetail is one session's share of a report's skip counters. The counters
// say how much input was unreadable; this says where, so a verbose warning can
// name the session to inspect or attach to a bug report.
type SkipDetail struct {
	SessionID  string
	Unreadable bool // the session's own transcript could not be read
	Lines      int  // JSONL lines skipped as malformed or oversized, agents included
	Agents     int  // agent sub-sessions that could not be read
}
