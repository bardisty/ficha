// Package formatter renders the complete reports that show, list, summary
// and global print, as tables, json or csv.
//
// A table fits the width its caller passes in; 0 means stdout isn't a
// terminal and selects the fixed layout. The package never detects the
// terminal itself, so a report is a pure function of its inputs, which is
// what makes the goldens in testdata possible.
//
// The small pieces a report is built from (cost and token strings, section
// rules, model names cut to a column) live in internal/render, because
// internal/tui draws them too.
package formatter
