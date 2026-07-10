package formatter

// csvCell neutralizes a string cell that a spreadsheet would evaluate as a
// formula rather than display as text. Spreadsheets treat a cell opening with
// =, +, -, @, tab, or CR as an expression; encoding/csv quotes for delimiters
// and newlines but does nothing about this, so a model ID read out of a
// transcript, or an agent ID read off a filename, could execute on open.
// Prefixing with an apostrophe is the standard escape and spreadsheets strip it
// on display. Non-string columns (counts, costs, timestamps) never begin with
// these characters and are written unwrapped.
//
// A neutralized cell no longer byte-matches its json counterpart, so a csv/json
// cross-format join on such a value needs the prefix stripped. Only
// formula-shaped values are ever touched; no real model or agent ID is.
func csvCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}
