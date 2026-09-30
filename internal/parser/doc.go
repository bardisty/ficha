// Package parser reads what Claude Code writes to disk: the project
// directories under the projects dir, each project's sessions-index.json,
// the JSONL session transcripts, and the agent and workflow-run transcripts
// beside them.
//
// It merges the index with the transcripts it finds, drops the repeated lines
// a streamed message leaves, and skips lines it can't read, counting them so
// commands can report the skips. It doesn't price anything; that is
// internal/analyzer's job.
package parser
