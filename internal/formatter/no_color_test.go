package formatter

import (
	"strings"
	"testing"
)

// --no-color must emit no escape codes. The formatter writes an escape for
// every styled call, whatever the terminal, so one left in a no-color branch
// shows up here. Both glyph sets.
func TestNoColorEmitsNoEscapes(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		if ascii {
			useASCII(t)
		}
		analysis, results := summaryDetailsAnalysis(t, summaryDetailsFixture(t))
		outputs := map[string]string{
			"show":            FormatSessionTable(goldenShowAnalysis(), true, 0),
			"show workflows":  FormatSessionTable(goldenShowWorkflowAnalysis(), true, 0),
			"summary":         FormatSessionTable(goldenSummaryAnalysis(), true, 0),
			"summary details": FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, false, 0),
			"summary expand":  FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, true, 0),
			"list":            FormatSessionListTable(goldenListResults(), true, goldenListOptions),
			"global":          FormatGlobalTable(goldenGlobalAnalysis(), true, goldenGlobalOptions(false)),
			"global details":  FormatGlobalTable(goldenGlobalAnalysis(), true, goldenGlobalOptions(true)),
		}
		for name, out := range outputs {
			if i := strings.Index(out, "\x1b["); i >= 0 {
				t.Errorf("ascii=%v %s: escape code in no-color output near %q", ascii, name, out[max(0, i-20):min(len(out), i+20)])
			}
		}
	}
}
