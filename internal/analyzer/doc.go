// Package analyzer turns transcripts into costs. Given a session file, it has
// internal/parser read it and the agent transcripts beside it, prices each
// message through internal/pricing, and rolls the agents into the session's
// totals. It builds the per-session, per-project and all-projects analyses
// that every command reports, plus the insights (trend, peak message) and the
// message list breakdown shows.
//
// Its results are the internal/models types. It prints nothing: the static
// reports are internal/formatter's, the live views internal/tui's.
package analyzer
