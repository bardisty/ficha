package formatter

import (
	"regexp"
	"testing"

	"github.com/bardisty/ficha/internal/styles"
	"github.com/muesli/termenv"
)

// darkOnlyIndexes are the xterm-256 colors ficha draws only on a dark
// background. Any of them in light-background output is a color that
// bypassed the palette.
var darkOnlyIndexes = map[string]bool{
	"42": true, "43": true, "75": true, "99": true, "196": true, "212": true, "213": true, "214": true,
	"220": true, "221": true, "240": true, "245": true, "248": true, "250": true, "252": true,
}

var fgIndex = regexp.MustCompile(`38;5;(\d+)`)

// On a light background every report draws from the light palette.
func TestLightBackgroundReports(t *testing.T) {
	forceProfile(t, termenv.ANSI256)
	styles.SetDark(false)
	t.Cleanup(func() { styles.SetDark(true) })

	analysis, results := summaryDetailsAnalysis(t, summaryDetailsFixture(t))
	for name, out := range map[string]string{
		"show":            FormatSessionTable(goldenShowAnalysis(), false, 0),
		"show workflows":  FormatSessionTable(goldenShowWorkflowAnalysis(), false, 0),
		"summary details": FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", false, true, 0),
		"list":            FormatSessionListTable(goldenListResults(), false, goldenListOptions),
		"global":          FormatGlobalTable(goldenGlobalAnalysis(), false, goldenGlobalOptions(true)),
	} {
		matches := fgIndex.FindAllStringSubmatch(out, -1)
		if len(matches) == 0 {
			t.Errorf("%s: no 256-color escapes, so the test checks nothing", name)
		}
		for _, m := range matches {
			if darkOnlyIndexes[m[1]] {
				t.Errorf("%s: dark-background color %s on a light background", name, m[1])
			}
		}
	}
}
