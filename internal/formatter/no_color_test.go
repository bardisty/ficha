package formatter

import (
	"strings"
	"testing"

	"github.com/muesli/termenv"
)

// --no-color must emit no escape codes even when the terminal supports
// color: every surface renders under a forced 256-color profile, where any
// styled call left in a no-color branch would show up. Both glyph sets.
func TestNoColorEmitsNoEscapes(t *testing.T) {
	forceProfile(t, termenv.ANSI256)
	for _, ascii := range []bool{false, true} {
		if ascii {
			useASCII(t)
		}
		analysis, results := summaryDetailsAnalysis(t, summaryDetailsFixture(t))
		outputs := map[string]string{
			"show":            FormatSessionTable(goldenShowAnalysis(), true),
			"show workflows":  FormatSessionTable(goldenShowWorkflowAnalysis(), true),
			"summary":         FormatSessionTable(goldenSummaryAnalysis(), true),
			"summary details": FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, false),
			"summary expand":  FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, true),
			"list":            FormatSessionListTable(goldenSessionEntries(), true),
			"global":          FormatGlobalTable(goldenGlobalAnalysis(), true, 3, false),
			"global details":  FormatGlobalTable(goldenGlobalAnalysis(), true, 3, true),
		}
		for name, out := range outputs {
			if i := strings.Index(out, "\x1b["); i >= 0 {
				t.Errorf("ascii=%v %s: escape code in no-color output near %q", ascii, name, out[max(0, i-20):min(len(out), i+20)])
			}
		}
	}
}
