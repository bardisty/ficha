// Package cmd is ficha's command line: the cobra commands, their flags and
// help, and everything that depends on the process ficha runs in.
//
// So cmd decides whether stdout and stderr are terminals and how wide stdout
// is; the static reports never check. It turns --no-color and NO_COLOR into
// the noColor every renderer takes, sets the --ascii glyph set and the light
// or dark palette in internal/styles, passes internal/formatter the width to
// fit, and cuts a table's color escapes down to what stdout can show. It also
// resolves which project a command means, from -p, --project-dir or the
// working directory, and when a session ID isn't in that project, names the
// project that has it and the command to run. internal/paths does the
// matching.
//
// Reading and costing transcripts belong to internal/parser and
// internal/analyzer, and drawing them to internal/formatter and internal/tui.
package cmd
