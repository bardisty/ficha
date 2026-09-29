package cmd

import "io"

// config holds every flag value and output sink for a single command run.
// newRootCmd builds one fresh config per invocation and binds all flags to it,
// so Execute is re-entrant: nothing (notably watch's live mode) leaks between
// runs, and an in-process caller can execute commands back to back.
type config struct {
	// Persistent flags (bound on the root command).
	format  string
	verbose bool
	noColor bool
	ascii   bool // --ascii: ASCII frames and symbols; independent of noColor

	// Project flags (root, show, watch, breakdown, list, summary).
	projectPath string
	projectDir  string // --project-dir: explicit Claude project directory name

	// Live flags (root, show, watch, breakdown).
	live     bool
	noFollow bool

	// show flags.
	messages bool // --messages: per-message rows/records in json/csv instead of the session summary

	// summary flags.
	showDetails  bool
	expandAgents bool

	// global flags.
	globalDetails bool
	globalTopN    int
	globalSortBy  string
	globalNoCache bool

	// Output sinks, resolved from the command in PersistentPreRunE. Point at
	// os.Stdout/os.Stderr in production and at test buffers when a test calls
	// SetOut/SetErr, so every command's output is capturable.
	stdout io.Writer
	stderr io.Writer

	// commandPath names the command as typed ("ficha watch", "ficha show
	// --live") for messages that refer to it.
	commandPath string
}
