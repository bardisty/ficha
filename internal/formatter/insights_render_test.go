package formatter

import (
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

// TestFormatInsights_TrendGate pins the trend gate on the show surface: the
// trend row must render only when the analyzer actually computed a trend
// (MessageCount >= models.MinMessagesForTrend). One message below the threshold
// the trend fields are still zero values, so a rendered row would be a
// fabricated "$0.00/msg -> $0.00/msg stable".
func TestFormatInsights_TrendGate(t *testing.T) {
	below := &models.MessageInsights{MessageCount: models.MinMessagesForTrend - 1}
	if out := formatInsightsSectionContent(below, false, 76, true); strings.Contains(out, "Trend") {
		t.Errorf("trend row rendered at %d messages (below threshold):\n%s", below.MessageCount, out)
	}

	at := &models.MessageInsights{
		MessageCount:  models.MinMessagesForTrend,
		CostTrend:     models.TrendIncreasing,
		RecentAvgCost: 0.25,
		TrendWindow:   3,
	}
	if out := formatInsightsSectionContent(at, false, 76, true); !strings.Contains(out, "Trend") {
		t.Errorf("trend row missing at %d messages (threshold):\n%s", at.MessageCount, out)
	}
}

// TestFormatInsights_ScopeLabel pins the scope label on the show surface: these
// insights are parent-transcript-only, so when agents ran the scope is labeled;
// without agents (parent == all messages) no label appears, so the common case
// stays unchanged.
func TestFormatInsights_ScopeLabel(t *testing.T) {
	insights := &models.MessageInsights{
		MessageCount: models.MinMessagesForTrend,
		FirstMessage: &models.MessageSnapshot{Index: 1},
	}
	if out := formatInsightsSectionContent(insights, true, 76, true); !strings.Contains(out, "main conversation only, agents excluded") {
		t.Errorf("expected scope label when agents present:\n%s", out)
	}
	if out := formatInsightsSectionContent(insights, false, 76, true); strings.Contains(out, "main conversation") {
		t.Errorf("did not expect scope label when no agents:\n%s", out)
	}
}
