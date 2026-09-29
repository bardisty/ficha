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

// unknownModelIDs lists, sorted and once each, the fallback-priced models in
// the breakdown's rows.
func (m BreakdownModel) unknownModelIDs() []string {
	seen := make(map[string]models.CostBreakdown)
	for _, msg := range m.messages {
		if !pricing.IsKnownModel(msg.Model) {
			seen[msg.Model] = models.CostBreakdown{}
		}
	}
	return unknownModelIDs(seen)
}
