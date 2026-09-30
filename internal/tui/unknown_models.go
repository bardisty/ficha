package tui

import (
	"sort"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
)

// unknownModelIDs lists, sorted, the models in a cost-by-model map that were
// priced from the fallback table.
func unknownModelIDs(costByModel map[string]models.CostBreakdown) []string {
	var ids []string
	for id := range costByModel {
		if !pricing.IsKnownModel(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
